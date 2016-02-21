// Copyright (c) 2014-2015 Ludovic Fauvet
// Licensed under the MIT license

package daemon

import (
	"errors"
	"fmt"
	"github.com/wsnipex/mirrorbits/cli"
	. "github.com/wsnipex/mirrorbits/config"
	"github.com/wsnipex/mirrorbits/core"
	"github.com/wsnipex/mirrorbits/database"
	"github.com/wsnipex/mirrorbits/mirrors"
	"github.com/wsnipex/mirrorbits/scan"
	"github.com/wsnipex/mirrorbits/utils"
	"github.com/garyburd/redigo/redis"
	"github.com/op/go-logging"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	healthCheckThreads = 10
	userAgent          = "Mirrorbits/" + core.VERSION + " PING CHECK"
	clientTimeout      = time.Duration(20 * time.Second)
	clientDeadline     = time.Duration(40 * time.Second)
	redirectError      = errors.New("Redirect not allowed")
	mirrorNotScanned   = errors.New("Mirror has not yet been scanned")

	log = logging.MustGetLogger("main")
)

type Monitor struct {
	redis           *database.Redis
	cache           *mirrors.Cache
	mirrors         map[string]*Mirror
	mapLock         sync.Mutex
	httpClient      http.Client
	httpTransport   http.Transport
	healthCheckChan chan string
	syncChan        chan string
	stop            chan bool
	configNotifier  chan bool
	wg              sync.WaitGroup
	formatLongestID int

	cluster *cluster
}

type Mirror struct {
	mirrors.Mirror
	checking  bool
	scanning  bool
	lastCheck int64
}

func (m *Mirror) NeedHealthCheck() bool {
	return utils.ElapsedSec(m.lastCheck, int64(60*GetConfig().CheckInterval))
}

func (m *Mirror) NeedSync() bool {
	return utils.ElapsedSec(m.LastSync, int64(60*GetConfig().ScanInterval))
}

func (m *Mirror) IsScanning() bool {
	return m.scanning
}

func (m *Mirror) IsChecking() bool {
	return m.checking
}

func NewMonitor(r *database.Redis, c *mirrors.Cache) *Monitor {
	monitor := new(Monitor)
	monitor.redis = r
	monitor.cache = c
	monitor.cluster = NewCluster(r)
	monitor.mirrors = make(map[string]*Mirror)
	monitor.healthCheckChan = make(chan string, healthCheckThreads*5)
	monitor.syncChan = make(chan string)
	monitor.stop = make(chan bool)
	monitor.configNotifier = make(chan bool, 1)

	SubscribeConfig(monitor.configNotifier)

	rand.Seed(time.Now().UnixNano())

	monitor.httpTransport = http.Transport{
		DisableKeepAlives:   true,
		MaxIdleConnsPerHost: 0,
		Dial: func(network, addr string) (net.Conn, error) {
			deadline := time.Now().Add(clientDeadline)
			c, err := net.DialTimeout(network, addr, clientTimeout)
			if err != nil {
				return nil, err
			}
			c.SetDeadline(deadline)
			return c, nil
		},
	}

	monitor.httpClient = http.Client{
		CheckRedirect: checkRedirect,
		Transport:     &monitor.httpTransport,
	}
	return monitor
}

func (m *Monitor) Stop() {
	select {
	case _, _ = <-m.stop:
		return
	default:
		m.cluster.Stop()
		close(m.stop)
	}
}

func (m *Monitor) Wait() {
	m.wg.Wait()
}

// Return an error if the endpoint is an unauthorized redirect
func checkRedirect(req *http.Request, via []*http.Request) error {
	if GetConfig().DisallowRedirects {
		return redirectError
	}
	return nil
}

// Main monitor loop
func (m *Monitor) MonitorLoop() {
	m.wg.Add(1)
	defer m.wg.Done()

	mirrorUpdateEvent := make(chan string, 10)
	m.redis.Pubsub.SubscribeEvent(database.MIRROR_UPDATE, mirrorUpdateEvent)

	// Scan the local repository
	m.retry(func() error {
		return m.scanRepository()
	}, 1*time.Second)

	// Synchronize the list of all known mirrors
	m.retry(func() error {
		ids, err := m.mirrorsID()
		if err != nil {
			return err
		}
		m.syncMirrorList(ids...)
		return nil
	}, 500*time.Millisecond)

	if utils.IsStopped(m.stop) {
		return
	}

	// Start the cluster manager
	m.cluster.Start()

	// Start the health check routines
	for i := 0; i < healthCheckThreads; i++ {
		go m.healthCheckLoop()
	}

	// Start the mirror sync routines
	for i := 0; i < GetConfig().ConcurrentSync; i++ {
		go m.syncLoop()
	}

	// Setup recurrent tasks
	var repositoryScanTicker <-chan time.Time
	repositoryScanInterval := -1
	mirrorCheckTicker := time.NewTicker(1 * time.Second)

	// Disable the mirror check while stopping to avoid spurious events
	go func() {
		select {
		case <-m.stop:
			mirrorCheckTicker.Stop()
		}
	}()

	// Force a first configuration reload to setup the timers
	select {
	case m.configNotifier <- true:
	default:
	}

	for {
		select {
		case <-m.stop:
			return
		case id := <-mirrorUpdateEvent:
			m.syncMirrorList(id)
		case <-m.configNotifier:
			if repositoryScanInterval != GetConfig().RepositoryScanInterval {
				repositoryScanInterval = GetConfig().RepositoryScanInterval

				if repositoryScanInterval == 0 {
					repositoryScanTicker = nil
				} else {
					repositoryScanTicker = time.Tick(time.Duration(repositoryScanInterval) * time.Minute)
				}
			}
		case <-repositoryScanTicker:
			m.scanRepository()
		case <-mirrorCheckTicker.C:
			if m.redis.Failure() {
				continue
			}
			m.mapLock.Lock()
			for k, v := range m.mirrors {
				if !v.Enabled {
					// Ignore disabled mirrors
					continue
				}
				if v.NeedHealthCheck() && !v.IsChecking() && m.cluster.IsHandled(k) {
					select {
					case m.healthCheckChan <- k:
						m.mirrors[k].checking = true
					default:
					}
				}
				if v.NeedSync() && !v.IsScanning() && m.cluster.IsHandled(k) {
					select {
					case m.syncChan <- k:
						m.mirrors[k].scanning = true
					default:
					}
				}
			}
			m.mapLock.Unlock()
		}
	}
}

// Returns a list of all mirrors ID
func (m *Monitor) mirrorsID() ([]string, error) {
	rconn := m.redis.Get()
	defer rconn.Close()

	return redis.Strings(rconn.Do("LRANGE", "MIRRORS", "0", "-1"))
}

// Sync the remote mirror struct with the local dataset
func (m *Monitor) syncMirrorList(mirrorsIDs ...string) error {

	for _, id := range mirrorsIDs {
		if len(id) > m.formatLongestID {
			m.formatLongestID = len(id)
		}
		mirror, err := m.cache.GetMirror(id)
		if err != nil && err != redis.ErrNil {
			log.Errorf("Fetching mirror %s failed: %s", id, err.Error())
			continue
		} else if err == redis.ErrNil {
			// Mirror has been deleted
			m.mapLock.Lock()
			delete(m.mirrors, id)
			m.mapLock.Unlock()
			m.cluster.RemoveMirror(&mirror)
			continue
		}

		m.cluster.AddMirror(&mirror)

		m.mapLock.Lock()
		if _, ok := m.mirrors[mirror.ID]; ok {
			// Update existing mirror
			tmp := m.mirrors[mirror.ID]
			tmp.Mirror = mirror
			m.mirrors[mirror.ID] = tmp
		} else {
			// Add new mirror
			m.mirrors[mirror.ID] = &Mirror{
				Mirror: mirror,
			}
		}
		m.mapLock.Unlock()
	}

	log.Debugf("%d mirror%s updated", len(mirrorsIDs), utils.Plural(len(mirrorsIDs)))
	return nil
}

// Main health check loop
// TODO merge with the monitorLoop?
func (m *Monitor) healthCheckLoop() {
	m.wg.Add(1)
	defer m.wg.Done()
	for {
		select {
		case <-m.stop:
			return
		case k := <-m.healthCheckChan:
			if utils.IsStopped(m.stop) {
				return
			}
			m.mapLock.Lock()
			mirror := m.mirrors[k]
			m.mapLock.Unlock()

			err := m.healthCheck(mirror.Mirror)

			if err == mirrorNotScanned {
				// Not removing the 'checking' lock is intended here so the mirror won't
				// be checked again until the rsync/ftp scan is finished.
				continue
			}

			m.mapLock.Lock()
			if _, ok := m.mirrors[k]; ok {
				if !database.RedisIsLoading(err) {
					m.mirrors[k].lastCheck = time.Now().UTC().Unix()
				}
				m.mirrors[k].checking = false
			}
			m.mapLock.Unlock()
		}
	}
}

// Main sync loop
// TODO merge with the monitorLoop?
func (m *Monitor) syncLoop() {
	m.wg.Add(1)
	defer m.wg.Done()
	for {
		select {
		case <-m.stop:
			return
		case k := <-m.syncChan:
			m.mapLock.Lock()
			mirror := m.mirrors[k]
			m.mapLock.Unlock()

			conn := m.redis.Get()
			scanning, err := scan.IsScanning(conn, k)
			if err != nil {
				conn.Close()
				if !database.RedisIsLoading(err) {
					log.Warningf("syncloop: %s", err.Error())
				}
				goto unlock
			} else if scanning {
				// A scan is already in progress on another node
				conn.Close()
				goto unlock
			}
			conn.Close()

			log.Debugf("Scanning %s", k)

			err = cli.NoSyncMethod

			// First try to scan with rsync
			if mirror.RsyncURL != "" {
				err = scan.Scan(scan.RSYNC, m.redis, mirror.RsyncURL, k, m.stop)
			}
			// If it failed or rsync wasn't supported
			// fallback to FTP
			if err != nil && err != scan.ScanAborted && mirror.FtpURL != "" {
				err = scan.Scan(scan.FTP, m.redis, mirror.FtpURL, k, m.stop)
			}

			if err == scan.ScanInProgress {
				log.Warningf("%-30.30s Scan already in progress", k)
				goto unlock
			}

			if mirror.Up == false {
				select {
				case m.healthCheckChan <- k:
				default:
				}
			}

		unlock:
			m.mapLock.Lock()
			if _, ok := m.mirrors[k]; ok {
				m.mirrors[k].scanning = false
			}
			m.mapLock.Unlock()
		}
	}
}

// Do an actual health check against a given mirror
func (m *Monitor) healthCheck(mirror mirrors.Mirror) error {
	// Format log output
	format := "%-" + fmt.Sprintf("%d.%ds", m.formatLongestID+4, m.formatLongestID+4)

	// Copy the stop channel to make it nilable locally
	stopflag := m.stop

	// Get the URL to a random file available on this mirror
	file, size, err := m.getRandomFile(mirror.ID)
	if err != nil {
		if err == redis.ErrNil {
			return mirrorNotScanned
		} else if !database.RedisIsLoading(err) {
			log.Warningf(format+"Error: Cannot obtain a random file: %s", mirror.ID, err)
		}
		return err
	}

	// Prepare the HTTP request
	req, err := http.NewRequest("HEAD", strings.TrimRight(mirror.HttpURL, "/")+file, nil)
	req.Header.Set("User-Agent", userAgent)
	req.Close = true

	done := make(chan bool)
	var resp *http.Response
	var elapsed time.Duration

	// Execute the request inside a goroutine to allow aborting the request
	go func() {
		start := time.Now()
		resp, err = m.httpClient.Do(req)
		elapsed = time.Since(start)

		if err == nil {
			resp.Body.Close()
		}

		done <- true
	}()

x:
	for {
		select {
		case <-stopflag:
			log.Debugf("Aborting health-check for %s", mirror.HttpURL)
			m.httpTransport.CancelRequest(req)
			stopflag = nil
		case <-done:
			if utils.IsStopped(m.stop) {
				return nil
			}
			break x
		}
	}

	if err != nil {
		if opErr, ok := err.(*net.OpError); ok {
			log.Debugf("Op: %s | Net: %s | Addr: %s | Err: %s | Temporary: %t", opErr.Op, opErr.Net, opErr.Addr, opErr.Error(), opErr.Temporary())
		}
		mirrors.MarkMirrorDown(m.redis, mirror.ID, "Unreachable")
		log.Errorf(format+"Error: %s (%dms)", mirror.ID, err.Error(), elapsed/time.Millisecond)
		return err
	}

	contentLength := resp.Header.Get("Content-Length")

	if resp.StatusCode == 404 {
		mirrors.MarkMirrorDown(m.redis, mirror.ID, fmt.Sprintf("File not found %s (error 404)", file))
		if GetConfig().DisableOnMissingFile {
			mirrors.DisableMirror(m.redis, mirror.ID)
		}
		log.Errorf(format+"Error: File %s not found (error 404)", mirror.ID, file)
	} else if resp.StatusCode != 200 {
		mirrors.MarkMirrorDown(m.redis, mirror.ID, fmt.Sprintf("Got status code %d", resp.StatusCode))
		log.Warningf(format+"Down! Status: %d", mirror.ID, resp.StatusCode)
	} else {
		mirrors.MarkMirrorUp(m.redis, mirror.ID)
		rsize, err := strconv.ParseInt(contentLength, 10, 64)
		if err == nil && rsize != size {
			log.Warningf(format+"File size mismatch! [%s] (%dms)", mirror.ID, file, elapsed/time.Millisecond)
		} else {
			log.Noticef(format+"Up! (%dms)", mirror.ID, elapsed/time.Millisecond)
		}
	}
	return nil
}

// Get a random filename known to be served by the given mirror
func (m *Monitor) getRandomFile(identifier string) (file string, size int64, err error) {
	sinterKey := fmt.Sprintf("HANDLEDFILES_%s", identifier)

	rconn := m.redis.Get()
	defer rconn.Close()

	file, err = redis.String(rconn.Do("SRANDMEMBER", sinterKey))
	if err != nil {
		return
	}

	size, err = redis.Int64(rconn.Do("HGET", fmt.Sprintf("FILE_%s", file), "size"))
	if err != nil {
		return
	}

	return
}

// Trigger a sync of the local repository
func (m *Monitor) scanRepository() error {
	err := scan.ScanSource(m.redis, m.stop)
	if err != nil {
		log.Error("Scanning source failed: %s", err.Error())
	}
	return err
}

// Retry a function until no errors is returned while still allowing
// the process to be stopped.
func (m *Monitor) retry(fn func() error, delay time.Duration) {
	for {
		err := fn()
		if err == nil {
			break
		}
		select {
		case <-m.stop:
			return
		case <-time.After(delay):
		}
	}
}

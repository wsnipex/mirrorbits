// Copyright (c) 2014-2015 Ludovic Fauvet
// Licensed under the MIT license

package daemon

import (
	"fmt"
	"github.com/wsnipex/mirrorbits/database"
	"github.com/wsnipex/mirrorbits/mirrors"
	"github.com/wsnipex/mirrorbits/utils"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	clusterAnnounce = "HELLO"
)

type cluster struct {
	redis *database.Redis

	nodes        []Node
	nodeIndex    int
	nodeTotal    int
	nodesLock    sync.RWMutex
	mirrorsIndex []string
	stop         chan bool
	wg           sync.WaitGroup
	running      bool
}

type Node struct {
	ID           string
	LastAnnounce int64
}

type ByNodeID []Node

func (n ByNodeID) Len() int           { return len(n) }
func (n ByNodeID) Swap(i, j int)      { n[i], n[j] = n[j], n[i] }
func (n ByNodeID) Less(i, j int) bool { return n[i].ID < n[j].ID }

func NewCluster(r *database.Redis) *cluster {
	c := &cluster{
		redis: r,
		nodes: make([]Node, 0),
		stop:  make(chan bool),
	}
	return c
}

func (c *cluster) Start() {
	if c.running == true {
		return
	}
	log.Debug("Cluster starting...")
	c.running = true
	c.wg.Add(1)
	c.stop = make(chan bool)
	go c.clusterLoop()
}

func (c *cluster) Stop() {
	select {
	case _, _ = <-c.stop:
		return
	default:
		close(c.stop)
		c.wg.Wait()
		c.running = false
		log.Debug("Cluster stopped")
	}
}

func (c *cluster) clusterLoop() {
	clusterChan := make(chan string, 10)
	announceTicker := time.NewTicker(1 * time.Second)

	hostname := utils.GetHostname()
	nodeID := fmt.Sprintf("%s-%05d", hostname, rand.Intn(32000))

	c.refreshNodeList(nodeID, nodeID)
	c.redis.Pubsub.SubscribeEvent(database.CLUSTER, clusterChan)

	for {
		select {
		case <-c.stop:
			c.wg.Done()
			return
		case <-announceTicker.C:
			r := c.redis.Get()
			database.Publish(r, database.CLUSTER, fmt.Sprintf("%s %s", clusterAnnounce, nodeID))
			r.Close()
		case data := <-clusterChan:
			if !strings.HasPrefix(data, clusterAnnounce+" ") {
				// Garbage
				continue
			}
			c.refreshNodeList(data[len(clusterAnnounce)+1:], nodeID)
		}
	}
}

func (c *cluster) refreshNodeList(nodeID, self string) {
	found := false

	c.nodesLock.Lock()

	// Expire unreachable nodes
	for i := 0; i < len(c.nodes); i++ {
		if utils.ElapsedSec(c.nodes[i].LastAnnounce, 5) && c.nodes[i].ID != nodeID && c.nodes[i].ID != self {
			log.Noticef("<- Node %s left the cluster", c.nodes[i].ID)
			c.nodes = append(c.nodes[:i], c.nodes[i+1:]...)
			i--
		} else if c.nodes[i].ID == nodeID {
			found = true
			c.nodes[i].LastAnnounce = time.Now().UTC().Unix()
		}
	}

	// Join new node
	if !found {
		if nodeID != self {
			log.Noticef("-> Node %s joined the cluster", nodeID)
		}
		n := Node{
			ID:           nodeID,
			LastAnnounce: time.Now().UTC().Unix(),
		}
		// TODO use binary search here
		// See https://golang.org/pkg/sort/#Search
		c.nodes = append(c.nodes, n)
		sort.Sort(ByNodeID(c.nodes))
	}

	c.nodeTotal = len(c.nodes)

	// TODO use binary search here
	// See https://golang.org/pkg/sort/#Search
	for i, n := range c.nodes {
		if n.ID == self {
			c.nodeIndex = i
			break
		}
	}

	c.nodesLock.Unlock()
}

func (c *cluster) AddMirror(mirror *mirrors.Mirror) {
	c.nodesLock.Lock()
	c.mirrorsIndex = addMirrorIDToSlice(c.mirrorsIndex, mirror.ID)
	c.nodesLock.Unlock()
}

func (c *cluster) RemoveMirror(mirror *mirrors.Mirror) {
	c.nodesLock.Lock()
	c.mirrorsIndex = removeMirrorIDFromSlice(c.mirrorsIndex, mirror.ID)
	c.nodesLock.Unlock()
}

func (c *cluster) IsHandled(mirrorID string) bool {
	c.nodesLock.RLock()
	index := sort.SearchStrings(c.mirrorsIndex, mirrorID)

	mRange := int(float32(len(c.mirrorsIndex))/float32(c.nodeTotal) + 0.5)
	start := mRange * c.nodeIndex
	c.nodesLock.RUnlock()

	// Check bounding to see if this mirror must be handled by this node.
	// The distribution of the nodes should be balanced except for the last node
	// that could contain one more node.
	if index >= start && (index < start+mRange || c.nodeIndex == c.nodeTotal-1) {
		return true
	}
	return false
}

func removeMirrorIDFromSlice(slice []string, mirrorID string) []string {
	// See https://golang.org/pkg/sort/#SearchStrings
	idx := sort.SearchStrings(slice, mirrorID)
	if idx < len(slice) && slice[idx] == mirrorID {
		slice = append(slice[:idx], slice[idx+1:]...)
	}
	return slice
}

func addMirrorIDToSlice(slice []string, mirrorID string) []string {
	// See https://golang.org/pkg/sort/#SearchStrings
	idx := sort.SearchStrings(slice, mirrorID)
	if idx >= len(slice) || slice[idx] != mirrorID {
		slice = append(slice[:idx], append([]string{mirrorID}, slice[idx:]...)...)
	}
	return slice
}

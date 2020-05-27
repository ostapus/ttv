package torc

import "C"
import (
	"bytes"
	tt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"io/ioutil"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"
	"ttv/logger"
)

// global
var (
	torClient = TorrentClient{configured: false}
	log       *logger.Log
)

type TorrentClient struct {
	tc       *tt.Client
	cfg      *tt.ClientConfig
	cw       chan Event
	loadDone chan bool
	//
	configured      bool
	DbDir           string
	TorrentsDir     string
	listenAddr      string
	listenPort      string
	PortForwardFile string
	ExternalPort    int
	ExternalAddr    string
	KodiCategory    string
	//
	torrents []*TorrentWithUserData
	//
	lock     sync.Mutex
	miscLock sync.Mutex
	wg       sync.WaitGroup
}

func NewTorrentClient(logger *logger.Log) *TorrentClient {
	if !torClient.configured {
		log = logger
		torClient.configured = true
		log.SetTraceFile(GetEnv("TC_TRACE", "/trace.conf"))
		torClient.DbDir = GetEnv("TC_DATADIR", "boltdb")
		torClient.TorrentsDir = GetEnv("TC_TORRENTSDIR", "torrents")
		torClient.listenAddr = GetEnv("TC_LISTENADDR", "0.0.0.0")
		torClient.listenPort = GetEnv("TC_LOCALPORT", "16881")
		torClient.PortForwardFile = GetEnv("TC_PORTFORWARDFILE", "/tmp/port_forward")
		if torClient.KodiCategory = GetEnv("TC_KODI_CATEGORY", ""); torClient.KodiCategory == "" {
			panic(log.Error("TC_KODI_CATEGORY is not defined"))
		}
		torClient.ExternalAddr = getExternalIP()
		torClient.ExternalPort = getExternalPort(torClient.PortForwardFile)
		torClient.torrents = make([]*TorrentWithUserData, 0)
		//
		torClient.cw = NewCategoryWatcher(torClient.TorrentsDir)
		torClient.loadDone = make(chan bool)
		go torClient.fileWatcher()

		torClient.cfg = tt.NewDefaultClientConfig()
		torClient.cfg.DefaultStorage = storage.NewFileWithCustomPathMaker(torClient.DbDir, customPathMaker)
		torClient.cfg.HTTPUserAgent = "Transmission/2.95"
		torClient.cfg.ExtendedHandshakeClientVersion = "Transmission/2.95"
		torClient.cfg.Bep20 = "-TR2950-"
		//
		torClient.cfg.ListenPort = torClient.ExternalPort
		torClient.cfg.PublicIp4 = net.ParseIP(torClient.ExternalAddr)
		//
		torClient.cfg.Debug = false
		torClient.cfg.DisableIPv6 = true
		torClient.cfg.DisableAcceptRateLimiting = true
		//
		if torClient.ExternalPort == 0 {
			torClient.ExternalPort = 16882
		}
		if c, err := tt.NewClient(torClient.cfg); err != nil {
			panic(log.Error("failed to create client: %v", err))
		} else {
			torClient.tc = c
		}
	}
	return &torClient
}

func monitorExternalAddrPort() {
	for {
		time.Sleep(time.Hour)
		newAddr := getExternalIP()
		newPort := getExternalPort(torClient.PortForwardFile)
		if (newAddr != "" && newPort != 0) && (newAddr != torClient.ExternalAddr || newPort != torClient.ExternalPort) {
			log.Warn("extern address or port changed, reloading client: was: %v:%v -> %v:%v", torClient.ExternalAddr, torClient.ExternalPort,
				newAddr, newPort)
			if torClient.ActivePlays() > 0 {
				log.Warn("keep client UP, %d active plays", torClient.ActivePlays())
				continue
			}
			log.Warn("exiting/restarting app to address port changes")
			torClient.tc.Close()
			os.Exit(10)
		}
	}
}

func (c *TorrentClient) Close() {
	c.tc.Close()
}

func (c *TorrentClient) fileWatcher() {
	for {
		ev := <-c.cw
		log.Info("%v", ev)
		switch ev.Op {
		case CategoryCreated:
			c.wg.Add(1)
			go c.AddTorrentFilesFromCategoryDir(ev.Category)
		case CategoryRemoved:
			log.Error("category %s has been removed, can't handle.. aborting client", ev.Category.name)
			c.Close()
		case CategoryLoaded:
			log.Info("CategoryLoading is done")

		case TorrentFileCreated:
			if strings.HasSuffix(ev.File, ".yaml") {
				if tu, _ := c.GetTorrent(ev.FullPath); tu != nil {
					tu.LoadTags()
				}
			} else {
				c.AddTorrentFromFile(ev.Category, ev.File, ev.FullPath)
			}
		case TorrentFileRemoved:
			if strings.HasSuffix(ev.File, ".torrent") {
				log.Error("%s deleted, marking to remove from client", ev.File)
				if tud, _ := c.GetTorrent(ev.FullPath); tud != nil {
					c.lock.Lock()
					defer c.lock.Unlock()
					tud.Tags.SetIfNew("delete_it", "yes")
					c.ProcessTags()
				}
			}
		}
	}
}

func (c *TorrentClient) Start() {
	log.Info("starting client on %v:%v", torClient.cfg.ListenHost, torClient.cfg.ListenPort)
	go monitorExternalAddrPort()

	go func() {
		log.Info("watiting on Initial scan to be done")
		c.wg.Wait()
		log.Info("initial scan is done, starting loop")
		for {
			time.Sleep(30 * time.Second)
			c.ProcessTags()
		}
	}()
}

func (c *TorrentClient) GetTorrent(hashOrNameOrIndex interface{}) (tud *TorrentWithUserData, index int) {
	log.Trace("GetTorrent: %v", hashOrNameOrIndex)
	index = -1
	tud = nil
	if len(c.torrents) > 0 {
		switch hashOrNameOrIndex.(type) {
		case string:
			hashOrName := hashOrNameOrIndex.(string)
			for i, tu := range c.torrents {
				if tu == nil {
					continue
				}
				if (tu.torrent != nil && tu.torrent.Name() == hashOrName) ||
					tu.Name == hashOrName ||
					tu.Tags.getString("infohash", "") == hashOrName ||
					tu.Tags.getString("fullpath", "wrone_one") == hashOrName {
					log.Trace("GetTorrent found by string: %s", tu.Name)
					tud = tu
					index = i
					break
				}
			}
		case int:
			val := hashOrNameOrIndex.(int)
			if val >= 0 && val < len(c.torrents) && c.torrents[val].InfoReady {
				log.Trace("GetTorrent found by index: %d", val)
				index = val
				tud = c.torrents[val]
			}
		}
	}
	log.Trace("GetTorrent tud_nil: %v index: %v", tud == nil, index)
	return
}

func (c *TorrentClient) AddTorrentFromFile(cat *tCategory, filename string, fullpath string) (tud *TorrentWithUserData, err error) {
	if st, err := os.Stat(fullpath); err != nil || st.IsDir() {
		err = newError(log.Warn("failed to stat %v or not file, ignore", fullpath))
	}
	if !IsValidTorrentFile(fullpath, true) {
		err = newError(log.Info("%s - IsValidTorrentFile fails, ignore", fullpath))
		return
	}
	info, err := ioutil.ReadFile(fullpath)
	if err != nil {
		err = newError(log.Info("%s - ReadFile : %s", fullpath, err))
		return
	}
	filename = strings.TrimSuffix(filename, ".torrent")
	filename = strings.TrimSuffix(filename, ".magnet")
	if tud, err = c.AddTorrentFromData(cat.name, filename, info, &Tags{}); err != nil {
		return
	}
	tags := tud.LoadTags()
	tags.SetIfNew("source", "from_file")
	tud.SetTags(tags)
	log.Debug("Verifying data for %s", tud.Name)
	tud.torrent.VerifyData()
	log.Debug("Verifying data for %s is done", tud.Name)
	log.Debug("AddTorrentFromFile completed: %s", tud.Name)
	log.Debug("\n%s", tud.Tags.String())
	tud.ProcessTags()
	tud.TrackProgress()
	return
}

func (c *TorrentClient) AddTorrentFromData(cat string, name string, info []byte, tags *Tags) (tud *TorrentWithUserData, err error) {
	log.Info("AddTorrentFromData: %s in %s", name, cat)

	c.lock.Lock()
	defer c.lock.Unlock()

	mi, err := metainfo.Load(bytes.NewReader(info))
	if err != nil {
		err = newError(log.Error("failed to metainfo.Load: %v", err))
		return
	}
	hash := mi.HashInfoBytes().HexString()
	log.Trace("hash: %v", hash)

	if tud, _ = c.GetTorrent(hash); tud != nil {
		log.Trace("%s already added, skipping", tud.Name)
		return
	}
	pcat := GetCategoryOrDefault(cat, c.KodiCategory)
	tname := path.Join(pcat.fullpath, name)
	if !strings.HasSuffix(tname, ".torrent") {
		tname += ".torrent"
	}
	tags.SetIfNew("name", name)
	tags.SetIfNew("category", pcat.name)
	tags.SetIfNew("download", pcat.download)
	tags.SetIfNew("fullpath", tname)
	tags.SetIfNew("tags_fullpath", tname+".tags.yaml")
	tags.SetIfNew("added", time.Now().Format(time.RFC822))
	tags.SetIfNew("infohash", hash)

	tud = NewTorrentWithUserData(tags)
	tud.Name = name
	tud.c = c
	done := false
	for i, v := range c.torrents {
		if v == nil {
			done = true
			c.torrents[i] = tud
			break
		}
	}
	if !done {
		c.torrents = append(c.torrents, tud)
	}

	tor, err := c.tc.AddTorrent(mi)
	if err != nil {
		c.torrents = c.torrents[:len(c.torrents)-1]
		err = newError(log.Error("failed to AddTorrent: %v", err))
		return
	}
	<-tor.GotInfo()
	if tor.Info().Private != nil {
		tud.Tags.SetIfNew("private", "yes")
		added := tud.Tags.getString("added", time.Now().Format(time.RFC822))
		d_added, err := time.Parse(time.RFC822, added)
		if err != nil {
			log.Error("failed to parse added date: '%s' : %v, using now", added, err)
			d_added = time.Now()
		}
		d_expiration := d_added.Add(time.Hour * 24 * 7 * 3)
		tud.Tags.SetIfNew("seed_until", d_expiration.Format(time.RFC822))
	}
	tud.Tags.SetIfNew("datapath", path.Join(pcat.download, tor.Name()))
	tud.torrent = tor
	tud.InfoReady = true
	tud.Pause()
	tud.SyncFiles()
	log.Info("%s added to client", tud.Name)
	return
}

func (c *TorrentClient) AddTorrentFilesFromCategoryDir(cat *tCategory) {
	c.miscLock.Lock()
	defer c.miscLock.Unlock()
	defer c.wg.Done()
	log.Info("scan torrents for %s in %s", cat.name, cat.fullpath)
	files, _ := ioutil.ReadDir(cat.fullpath)
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".yaml") {
			continue
		}
		notify <- Event{
			Category: cat,
			Op:       TorrentFileCreated,
			File:     file.Name(),
			FullPath: path.Join(cat.fullpath, file.Name()),
		}
	}
}

func (c *TorrentClient) GetTorrents() []*TorrentWithUserData {
	return c.torrents
}

func (c *TorrentClient) PauseNotInPlay() {
	for _, t := range c.torrents {
		if !(t.InPlay() || t.Completed() || t.ForceDownload) {
			t.Pause()
		}
	}
}

func (c *TorrentClient) ActivePlays() (count int) {
	for _, t := range c.torrents {
		if t.InPlay() && !t.Completed() {
			count += t.ActiveReaders()
		}
	}
	return
}

func (c *TorrentClient) RemoveTorrent(torrentId interface{}) {
	tud, index := c.GetTorrent(torrentId)
	if tud != nil {
		if tud.torrent != nil {
			tud.torrent.Drop()
		}
		c.torrents[index] = c.torrents[len(c.torrents)-1] // Copy last element to index i.
		c.torrents[len(c.torrents)-1] = nil               // Erase last element (write zero value).
		c.torrents = c.torrents[:len(c.torrents)-1]       // Truncate slice.	}
	}
}

func (c *TorrentClient) ProcessTags() {
	for _, tor := range c.torrents {
		if tor != nil {
			tor.ProcessTags()
		}
	}
}

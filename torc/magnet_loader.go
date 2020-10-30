package torc

import (
	"bytes"
	alog "github.com/anacrolix/log"
	tt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
	"os"
)

type MagnetLoader struct {
	tc  *tt.Client
	cfg *tt.ClientConfig
}

func NewMagnetLoader(dldir string) *MagnetLoader {
	if err := os.MkdirAll(dldir, os.ModePerm); err != nil {
		log.Error("Failed to create DL Dir: %s : %v... using /tmp/", dldir, err)
		dldir = "/tmp"
	}
	rc := MagnetLoader{}
	rc.cfg = tt.NewDefaultClientConfig()
	rc.cfg.DefaultStorage = storage.NewFile(dldir)
	rc.cfg.HTTPUserAgent = "Transmission/2.95"
	rc.cfg.ExtendedHandshakeClientVersion = "Transmission/2.95"
	rc.cfg.Bep20 = "-TR2950-"
	rc.cfg.ListenPort = 9876
	rc.cfg.Debug = false
	rc.cfg.DisableIPv6 = true
	rc.cfg.DisableAcceptRateLimiting = true
	rc.cfg.Logger = rc.cfg.Logger.FilterLevel(alog.Info)

	if c, err := tt.NewClient(rc.cfg); err != nil || c == nil {
		panic(log.Error("failed to create NewMagnetLoader: %v", err))
	} else {
		rc.tc = c
	}
	return &rc
}

func (c *MagnetLoader) LoadMagnet(magnet string) (mi []byte, err error) {
	log.Debug("LoadMagnet: %v", magnet)
	torrent, err := c.tc.AddMagnet(magnet)
	if err != nil {
		log.Error("Failed to add magnet: %v : %v", magnet, err)
		return nil, err
	}
	log.Debug("waiting on metainfo for %v", torrent.Name())
	<-torrent.GotInfo()
	log.Debug("metainfo loaded for %v", torrent.Name())

	var bb bytes.Buffer
	if err = torrent.Metainfo().Write(&bb); err != nil {
		log.Error("Failed to write Metainfo to Bytes.Buffer: %v", err)
		return nil, err
	}
	log.Debug("LoadMagnet torrents list")
	for i, v := range c.tc.Torrents() {
		log.Debug("   %v: %v", i, v.Name())
	}
	torrent.DisallowDataDownload()
	torrent.Drop()
	log.Debug("torrent %v removed from LoadMagnet client", torrent.Name())
	log.Debug("LoadMagnet torrents list after removal")
	for i, v := range c.tc.Torrents() {
		log.Debug("   %v: %v", i, v.Name())
	}
	return bb.Bytes(), nil
}

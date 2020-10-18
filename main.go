package main

import (
	"os"
	"ttv/logger"
	"ttv/torc"
)

var (
	log = logger.Log{}
)

func main() {
	log.InitLogger(os.Stderr)
	log.Level(logger.DEBUG)

	//os.Setenv("TC_DATADIR", "bolt_db")
	//os.Setenv("TC_TORRENTSDIR", "torrents")
	//os.Setenv("TC_LISTENADDR", "0.0.0.0")
	//os.Setenv("TC_LOCALPORT", "16882")
	//os.Setenv( "TC_PORTFORWARDFILE", "/tmp/port_forward")
	//os.Setenv( "TC_HTTPADDR", "0.0.0.0")
	//os.Setenv("TC_HTTPPORT", "3003")
	//os.Setenv("TC_CACHEDIR", "./cache")
	//os.Setenv("TC_KODI_CATEGORY", "kodi")
	//os.Setenv("TC_TRACE", "/trace_conf")
	tc := torc.NewTorrentClient(&log)
	//
	//{
	//	uri := "magnet:?xt=urn:btih:8A987AEA1545491112D1C70AC299304EF98CCC15&dn=trolls-world-tour-2020&tr=udp://open.demonii.com:1337/announce&tr=udp://tracker.openbittorrent.com:80&tr=udp://tracker.coppersurfer.tk:6969&tr=udp://glotorrents.pw:6969/announce&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://torrent.gresille.org:80/announce&tr=udp://p4p.arenabg.com:1337&tr=udp://tracker.leechers-paradise.org:6969"
	//	<-tc.LoadDone
	//	_mi, err := tc.LoadMetaInfoFromMagnet(uri)
	//	log.Debug("%s: %s", _mi, err)
	//	tud, err := tc.AddTorrentFromData("video", "trolls-world-tour-2020", _mi, &torc.Tags{})
	//	tud.Resume("force download")
	//	time.Sleep(time.Second*60)
	//	return
	//}
	tc.Start()
	srv := torc.NewHttpServer(tc)
	srv.Start()
	<-srv.Closed()

	tc.Close()
}

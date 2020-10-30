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
	//	<- tc.LoadDone
	//	uri := "magnet:?xt=urn:btih:B127082DEC04240FB9D617C23BFA3DF47E2DC0C7&dn=%5Bzooqle.com%5D%20Home%20Alone%20%281990%29%20720p%20BrRip%20x264%20-%20YIFY&tr=http://thetracker.org/announce&tr=http://bt1.archive.org:6969/announce&tr=http://bt2.archive.org:6969/announce&tr=http://tracker.tntvillage.scambioetico.org:2710/announce&tr=http://tracker.etree.org:6969/announce"
	//	_mi, err := tc.LoadMetaInfoFromMagnet(uri, "HomeAloneWhoCares")
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

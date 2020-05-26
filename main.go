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

	tc.Start()
	srv := torc.NewHttpServer(tc)
	srv.Start()
	<- srv.Closed()

	tc.Close()
}

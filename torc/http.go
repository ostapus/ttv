package torc

import (
	"encoding/json"
	"fmt"
	"github.com/anacrolix/missinggo"
	"github.com/gorilla/mux"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const TMDB_URL = `https://api.themoviedb.org/3`
const tmdb_API_KEY = `09c7d97911b06031f851d23554154de2`

const JACK_URL = `http://linux:9117`
const jack_API_KEY = `tnwamc70lv1xygeh9f7v5v71a739u0re`

type HttpServer struct {
	r          *mux.Router
	tc         *TorrentClient
	ListenAddr string
	ListenPort int64
	configured bool
	done       missinggo.Event
}

var (
	srv   = HttpServer{}
	rr    = mux.NewRouter()
	tc    *TorrentClient
	cache *Cache
)

func NewHttpServer(torClient *TorrentClient) *HttpServer {
	if srv.configured {
		return &srv
	}
	srv.ListenAddr = GetEnv("TC_HTTPADDR", "0.0.0.0")
	srv.ListenPort, _ = strconv.ParseInt(GetEnv("TC_HTTPPORT", "3003"), 10, 64)
	cache = NewCache(GetEnv("TC_CACHEDIR", "./cache"))
	srv.configured = true
	srv.r = rr
	srv.tc = torClient
	tc = torClient
	//
	rr.HandleFunc("/", _Home)
	rr.HandleFunc("/list", _List)
	rr.HandleFunc("/torrent_file_list", _torrentFileList)
	rr.HandleFunc("/playPrepare/{name}/{file}", _playPrepare)
	rr.HandleFunc("/torrentStatus/{name}", _torrentStatus)
	rr.HandleFunc("/play/{name}/{file}", _Play)
	rr.HandleFunc("/tag/{name}", _tagTorrent)
	rr.HandleFunc("/watchLaterList", _watchLaterList)
	//
	rr.HandleFunc("/api/tmdb", _ApiTmdb)
	rr.HandleFunc("/api/jacket", _ApiJacket)
	//
	rr.Use(loggingMiddleware)
	return &srv
}

type httpE struct {
	Error string `json:"error"`
}

func httpError(w http.ResponseWriter, status int, f string, v ...interface{}) (msg string) {
	msg = fmt.Sprintf(f, v...)
	_je, _ := json.Marshal(httpE{Error: msg})
	log.Error(string(_je))
	w.WriteHeader(status)
	w.Header().Set("Content-Type", "application/json")
	w.Write(_je)
	return msg
}

func doGet(req string) (data []byte, magnet string, err error) {
	baseUrl, err := url.Parse(req)
	magnet = ""
	if err != nil {
		err = newError("Failed to parse url: %v", err)
		return
	}
	cl := http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if strings.HasPrefix(req.URL.Scheme, "magnet") {
			log.Debug("redirected to %s", req.URL.Scheme)
			magnet = req.URL.String()
			return http.ErrUseLastResponse
		}
		return nil
	}}
	resp, err := cl.Get(baseUrl.String())
	if err != nil {
		return
	}

	if resp.StatusCode != http.StatusOK {
		err = newError("error: expected StatusOk, got %d - %s", resp.StatusCode, resp.Status)
		return
	}
	if magnet == "" {
		data, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			err = newError("ReadAll failed: %v", err)
			return
		}
	}
	return
}

func (s *HttpServer) Closed() <-chan struct{} {
	return s.done.C()
}

func (s *HttpServer) Start() {
	go func() {
		http.Handle("/", rr)
		http.ListenAndServe(fmt.Sprintf("%v:%v", s.ListenAddr, s.ListenPort), nil)
		s.done.Set()
	}()
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, _ := url.QueryUnescape(r.URL.RequestURI())
		log.Debug("< " + r.Method + " " + q)
		//		log.Debug(r.RequestURI)
		for k, v := range r.Header {
			log.Trace(fmt.Sprintf("    '%s' : '%s'", k, v))
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		next.ServeHTTP(w, r)

		log.Trace("> " + r.Host)
		for k, v := range w.Header() {
			log.Trace(fmt.Sprintf("    '%s' : '%s'", k, v))
		}
	})
}

func _Home(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, "<html><pre>")
	err := rr.Walk(func(route *mux.Route, router *mux.Router, ancestor []*mux.Route) error {
		pathTemplate, err := route.GetPathTemplate()
		if err == nil {
			fmt.Fprintln(w, "ROUTE:", pathTemplate)
		}
		pathRegexp, err := route.GetPathRegexp()
		if err == nil {
			fmt.Fprintln(w, "Path regexp:", pathRegexp)
		}
		queriesTemplates, err := route.GetQueriesTemplates()
		if err == nil {
			fmt.Fprintln(w, "Queries templates:", strings.Join(queriesTemplates, ","))
		}
		queriesRegexps, err := route.GetQueriesRegexp()
		if err == nil {
			fmt.Fprintln(w, "Queries regexps:", strings.Join(queriesRegexps, ","))
		}
		methods, err := route.GetMethods()
		if err == nil {
			fmt.Fprintln(w, "Methods:", strings.Join(methods, ","))
		}
		fmt.Println("</pre>")
		return nil
	})
	if err != nil {
		fmt.Fprint(w, "Error: %v", err)
	}
}

func _List(w http.ResponseWriter, r *http.Request) {
	rc := struct {
		Torrents []TorrentInfo `json:"Torrents"`
	}{
		Torrents: make([]TorrentInfo, 0),
	}
	for _, tu := range tc.GetTorrents() {
		td := tu.TorrentInfo()
		rc.Torrents = append(rc.Torrents, td)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rc)

	//
	//	buf := bytes.NewBufferString("")
	//	tc.tc.WriteStatus(buf)
	//	log.Debug("\n%s", buf.String())
}

func _torrentFileList(w http.ResponseWriter, r *http.Request) {
	var link string
	var tname string
	if tname = r.FormValue("name"); tname == "" {
		log.Error(httpError(w, http.StatusBadRequest, "torrent's name is missing"))
		return
	}
	log.Info("GetTorrent '%s'", tname)
	tor, _ := tc.GetTorrent(tname)
	if tor == nil {
		if link = r.FormValue("link"); link == "" {
			log.Error(httpError(w, http.StatusBadRequest, "link is missing"))
			return
		}

		log.Debug("loading link %s for %s", link, tname)
		metainfo, magnet, err := doGet(link)
		if err != nil {
			log.Error(httpError(w, http.StatusBadRequest, "failed to load torrent from Jackett: %v", err))
			return
		}
		if magnet != "" {
			log.Debug("Loading metainfo for magnet: %s", magnet)
			if metainfo, err = tc.LoadMetaInfoFromMagnet(magnet); err != nil {
				log.Error(httpError(w, http.StatusBadRequest, "failed lot load metadata for magnet: %s", err))
				return
			}
		}

		tags := &Tags{
			"source": "kodi",
		}
		tor, err = tc.AddTorrentFromData(tc.KodiCategory, tname, metainfo, tags)
		if tor == nil && err != nil {
			log.Error(httpError(w, http.StatusBadRequest, "AddTorrentFromData: %s failed: %v", tname, err))
			return
		}
	}
	jrc, err := json.Marshal(tor.TorrentInfo())
	if err != nil {
		log.Error(httpError(w, http.StatusInternalServerError, "couldn't convert to json "+err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(jrc)
}

func _torrentStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name, ok := vars["name"]
	if !ok {
		log.Error(httpError(w, http.StatusBadRequest, "missing torrent name"))
		return
	}
	tu, _ := tc.GetTorrent(name)
	if tu == nil {
		log.Error(httpError(w, http.StatusBadRequest, "failed to find torrent '%s'", name))
		return
	}

	ti := tu.TorrentInfo()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ti)
}

func _playPrepare(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name, ok := vars["name"]
	if !ok {
		log.Error(httpError(w, http.StatusBadRequest, "missing torrent name"))
		return
	}
	fname, ok := vars["file"]
	if !ok {
		log.Error(httpError(w, http.StatusBadRequest, "missing file name for '%v'", name))
		return
	}
	fname, _ = url.QueryUnescape(fname)
	tu, _ := tc.GetTorrent(name)
	if tu == nil {
		log.Error(httpError(w, http.StatusBadRequest, "failed to find torrent '%v'", name))
		return
	}
	tfile := tu.GetFile(fname)
	if tfile == nil {
		log.Error(httpError(w, http.StatusBadRequest, "failed to find tfile '%v' in %v", fname, name))
		return
	}

	tc.PauseNotInPlay()
	go tfile.PrepareForPlay()
	w.WriteHeader(http.StatusAccepted)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte("{\"status\":\"started\"}"))
}

var id = 0

func _Play(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name, ok := vars["name"]
	if !ok {
		log.Error(httpError(w, http.StatusBadRequest, "missing torrent name"))
		return
	}
	name, _ = url.QueryUnescape(name)
	fname, ok := vars["file"]
	if !ok {
		log.Error(httpError(w, http.StatusBadRequest, "missing file name for '%s'", name))
		return
	}

	fname, _ = url.QueryUnescape(fname)
	tu, _ := tc.GetTorrent(name)
	if tu == nil {
		log.Error(httpError(w, http.StatusBadRequest, "failed to find torrent '%s'", name))
		return
	}

	file := tu.GetFile(fname)
	if file == nil {
		log.Error(httpError(w, http.StatusBadRequest, "failed to find file '%v' in '%v'", fname, name))
		return
	}

	// play
	if r.Method == "HEAD" {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", strconv.FormatInt(file.file.Length(), 10))
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Connection", "Keep-Alive")
		w.Write([]byte(" "))
		return
	}

	if r.Method == "GET" {
		tc.PauseNotInPlay()
		rdr := file.OpenFileReader()
		start := time.Now()
		id += 1
		defer func() {
			log.Info("stream %d done after: %v sec", id, time.Since(start).Seconds())
			file.CloseFileReader(rdr)
		}()

		log.Info("starting stream id: %d for %v - %v", id, tu.torrent.Name(), file.file.Path())

		// w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Connection", "Keep-Alive")
		w.Header().Set("Content-Length", strconv.FormatInt(file.file.Length(), 10))
		w.Header().Set("Content-Type", "application/octet-stream")
		// io.Copy(w, rdr)
		http.ServeContent(w, r, file.file.Path(), time.Now(), rdr)
	}
}

func _tagTorrent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		log.Error(httpError(w, http.StatusInternalServerError, "failed to parse form "+err.Error()))
		return
	}
	vars := mux.Vars(r)
	tname, ok := vars["name"]
	if !ok {
		log.Error(httpError(w, http.StatusBadRequest, "missing torrent name"))
		return
	}
	tname, _ = url.QueryUnescape(tname)
	tu, _ := tc.GetTorrent(tname)
	if tu == nil {
		log.Error(httpError(w, http.StatusBadRequest, "failed to find torrent %s", tname))
		return
	}
	tags := make(Tags, 0)
	for k, v := range r.Form {
		tags.Set(k, v[0])
	}
	log.Debug("tag: %s -> %v", tname, tags)
	tu.AddTags(&tags)
	tc.ProcessTags()

	w.WriteHeader(http.StatusOK)
}

func _watchLaterList(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func _ApiTmdb(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		log.Error(httpError(w, http.StatusBadRequest, "Failed to parse form. bad request?"))
		return
	}
	q := r.URL.Query()
	key := q.Encode()

	ttl := 5 * time.Minute
	if q.Get("ttl") != "" {
		v, _ := strconv.Atoi(q.Get("ttl"))
		ttl = time.Duration(v) * time.Minute
		q.Del("ttl")
	}
	path := q.Get(`path`)
	q.Del(`path`)

	data, err := cache.Read(key)
	if err != nil {
		log.Error(httpError(w, http.StatusInternalServerError, err.Error()))
		return
	}
	if data != nil {
		log.Debug("cached result %v bytes", len(data))
		w.Write(data)
		return
	}

	// proxy request
	req, err := http.NewRequest("GET", TMDB_URL+path, nil)
	if err != nil {
		log.Error(httpError(w, http.StatusInternalServerError, err.Error()))
		return
	}
	q.Add(`api_key`, tmdb_API_KEY)
	q.Add(`include_adult`, "false")
	req.URL.RawQuery = q.Encode()
	log.Debug("proxy request to %v", req.URL.String())

	resp, err := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK || err != nil {
		err = newError("Bad response " + resp.Status)
	}
	if err != nil {
		log.Error(httpError(w, http.StatusInternalServerError, err.Error()))
		return
	}
	data, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error(httpError(w, http.StatusInternalServerError, err.Error()))
		return
	}
	// cache data
	_ = cache.Write(key, data, ttl)
	w.Write(data)
}

func _ApiJacket(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		log.Error(httpError(w, http.StatusBadRequest, "Failed to parse form. bad request?"))
		return
	}
	q := r.URL.Query()
	key := q.Encode()

	ttl := 10 * time.Minute
	if q.Get("ttl") != "" {
		v, _ := strconv.Atoi(q.Get("ttl"))
		ttl = time.Duration(v) * time.Minute
		q.Del("ttl")
	}
	path := q.Get(`path`)
	q.Del(`path`)

	data, err := cache.Read(key)
	if err != nil {
		log.Error(httpError(w, http.StatusInternalServerError, err.Error()))
		return
	}
	if data != nil {
		log.Debug("cached result %v bytes", len(data))
		w.Write(data)
		return
	}
	// proxy request
	req, err := http.NewRequest("GET", JACK_URL+path, nil)
	if err != nil {
		log.Error(httpError(w, http.StatusInternalServerError, err.Error()))
		return
	}
	q.Add(`apikey`, jack_API_KEY)
	req.URL.RawQuery = q.Encode()
	log.Debug("proxy request to %v", req.URL.String())

	cl := http.Client{}
	resp, err := cl.Do(req)
	if err != nil {
		log.Error(httpError(w, http.StatusInternalServerError, err.Error()))
		return
	}
	defer resp.Body.Close()
	data, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error(httpError(w, http.StatusInternalServerError, err.Error()))
		return
	}
	// cache data
	cache.Write(key, data, ttl)
	w.Write(data)
}

package main
import (
	"bytes"
	"fmt"
	tt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"io/ioutil"
)

func main() {
	torrent := "/home/mtv/Projects/go/ttv/data/Rush.Hour.Trilogy.1080p.BluRay.x264-nikt0 [IPT].torrent"
	data, _ := ioutil.ReadFile(torrent)
	mi, _ := metainfo.Load(bytes.NewReader(data))
	ts := tt.TorrentSpecFromMetaInfo(mi)
	fmt.Print(ts)
}
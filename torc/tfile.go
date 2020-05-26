package torc

import (
	tt "github.com/anacrolix/torrent"
	"io"
)

type TorrentFileInfo struct {
	Name string					`json:"Name"`
	Size int64					`json:"Size"`
	Ready bool					`json:"Ready"`
	BytesWant int				`json:"BytesWant"`
	BytesHave int				`json:"BytesHave"`
}

type TorrentFile struct {
	file *tt.File
	Tud *TorrentWithUserData
	Preparing bool
	BytesWant int
	BytesHave int
	ReadersOpen int
}

func NewTorrentFile(tud *TorrentWithUserData, file *tt.File) *TorrentFile {
	ps := int(tud.torrent.Info().PieceLength)
	rc := TorrentFile {
		Preparing: false,
		BytesWant: ps*LOAD_FROM_START+ps*LOAD_FROM_END,
		BytesHave: 0,
		Tud: tud,
		file: file,
		ReadersOpen: 0,
	}

	return &rc
}

func ( f *TorrentFile) Info() TorrentFileInfo {
	return TorrentFileInfo {
		Name: f.file.DisplayPath(),
		Size: f.file.Length(),
		Ready: f.Ready(),
		BytesWant: f.BytesWant,
		BytesHave: f.BytesHave,
	}
}

func (f *TorrentFile) OpenFileReader() (reader tt.Reader) {
	f.Tud.Resume("OpenFileReader")
	f.ReadersOpen += 1
	reader = f.file.NewReader()
	cs := f.Tud.torrent.Info().PieceLength
	reader.SetReadahead(cs*20)
	reader.SetResponsive()
	log.Info("open file reader %s, now active: %d",f.file.DisplayPath(), f.ReadersOpen )
	return
}

func (f *TorrentFile) CloseFileReader(reader tt.Reader)  {
	f.ReadersOpen -= 1
	_ = reader.Close()
	log.Info("close file reader %s, now active: %d",f.file.DisplayPath(), f.ReadersOpen)
}

func (f *TorrentFile) Ready() bool {
	return f.BytesHave >= f.BytesWant
}

func (f *TorrentFile) PrepareForPlay() {
	if f.Preparing {
		return
	}
	if f.Ready() {
		return
	}

	tu := f.Tud
	f.Preparing = true
	tu.Resume("prepare for play")
	cs := int64(tu.torrent.Info().PieceLength*LOAD_FROM_END)
	rdr := f.OpenFileReader()
	defer func() {
		f.Preparing = false
		f.CloseFileReader(rdr)
	}()

	buf := make([]byte, cs)
	log.Debug("reading from end %v ", cs)
	if _,err := rdr.Seek(-cs, io.SeekEnd); err != nil {
		log.Warn("failed to seek to io.SeekEnd: %v. ignore error", err)
	}
	for of := int64(0); of < cs; {
		n, err := rdr.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Error("failed to read %v - %v", f.file.Path(), err)
		}
		of += int64(n)
		f.BytesHave += n
		log.Debug("end: read/left: %v/%v", of, cs-of)
	}
	cs = tu.torrent.Info().PieceLength*LOAD_FROM_START
	if _, err := rdr.Seek(0, io.SeekStart); err != nil {
		log.Warn("failed to seek to io.SeekStart: %v. ignore error", err)
	}
	log.Debug("reading from start %v ", cs)
	for of := int64(0); of < cs; {
		n, err := rdr.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Error("failed to read %v - %v", f.file.Path(), err)
		}
		of += int64(n)
		f.BytesHave += n
		log.Debug("start: read/left: %v/%v", of, cs-of)
	}
	f.BytesHave = f.BytesWant
}



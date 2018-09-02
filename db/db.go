/*
Functions for manipulating and persisting the database of master media files.

Outside of this package, code will mostly want to interact with a MediaDB
object.  It consists of the file root (so that it can be re-scanned without
specifying it again) and a list of MasterFile objects.
*/

package db

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var Dbfile = flag.String("dbfile", "~/.mtooldb", "JSON cache file")

type MediaType uint8

const (
	Unknown MediaType = iota
	Video
	Audio
	Subtitle
)

type MpegStreamDesc struct {
	Type     MediaType
	Codec    string
	Language string
	Bitrate  int
}

type MpegDesc struct {
	Title   string
	Show    string
	Episode string
	Date    string
	Streams []MpegStreamDesc
}

type ImageFile struct {
	ImagePath   string
	ImageMtime  time.Time
	MasterPath  string
	MasterMtime time.Time
	Title       string
	Date        string
	HasPicture  bool
	Artist      string
	Album       string
	Track       int
	Stream      []MpegStreamDesc
}

// MasterFile represents one master file, which is the preimage to one or more
// derivative files.  If it is ever possible for a master file to be modified
// by this code, it is a bug.
type MasterFile struct {
	Path       string
	Type       MediaType
	Title      []string
	Date       string
	Mtime      time.Time
	HasPicture bool
	Valid      bool   // used for mark-and-sweep garbage collection
	Artist     string // only likely to be useful when Type == Audio
	Album      string
	TrackNum   int
	TrackMax   int
	Show       string // only likely to be useful when Type == Video
	Episode    string
	Stream     []MpegStreamDesc
}

// a MediaDB is just a list of MasterFiles, plus we save the FileRoot so that
// you can re-walk the directory without specifying it again.
type MediaDB struct {
	FileRoot    string
	MasterFiles []MasterFile
}

// a MasterFileHandler is a function that does the format-specific inspection
// to populate the metadata database.  These are fragile and have a lot of
// fragile dependencies, so they are separated into modules for easier
// maintenance.
type MasterFileHandler func(*MasterFile) error

// the handlerByExt map will be used to determine which intake handler to
// invoke on each master file in the library.
var HandlerByExt = map[string]MasterFileHandler{
	"flac": inspectFlac,
	"mkv":  inspectMpeg,
	"m4a":  inspectMp3,
	"mp3":  inspectMp3,
	"mp4":  inspectMpeg,
}

// compact() deletes all of the MasterFiles in mdb where Valid == false.
// Note that it does not delete any actual files from disk.
func (mdb *MediaDB) compact() {
	newlist := make([]MasterFile, 0, len(mdb.MasterFiles))
	for _, mf := range mdb.MasterFiles {
		if mf.Valid {
			newlist = append(newlist, mf)
		}
	}
	mdb.MasterFiles = newlist
	return
}

// RegexSearch() returns the paths of all master files where any metadata
// field matches the given regex.
func (mdb *MediaDB) RegexSearch(re string) (paths []string, err error) {

	for _, mf := range mdb.MasterFiles {
		var fields []string
		fields = append(fields, mf.Artist, mf.Album, mf.Date, mf.Show)
		fields = append(fields, mf.Title...)
		for _, field := range fields {
			matched, err := regexp.MatchString(re, field)
			if err != nil {
				return paths, err
			}
			if matched {
				paths = append(paths, mf.Path)
				// no need to keep matching against other metadata fields,
				// which only leads to duplicates in the output
				break
			}
		}
	}
	return paths, nil
}

// Latest() returns the paths of the n newest master files sorted by mtime.
func (mdb *MediaDB) Latest(n int) (paths []string) {

	sort.Slice(mdb.MasterFiles,
		func(i, j int) bool {
			return mdb.MasterFiles[i].Mtime.Unix() > mdb.MasterFiles[j].Mtime.Unix()
		})
	paths = make([]string, n, n)
	for i := range paths {
		paths[i] = mdb.MasterFiles[i].Path
	}
	return paths

}

// ExpandTilde() replaces the first ~ in path with the current user's home
// directory.
func ExpandTilde(path string) (string, error) {
	usr, err := user.Current()
	if err != nil {
		return path, err
	}
	return strings.Replace(path, "~", usr.HomeDir, 1), nil
}

// LoadDb() parses the cache file on disk and returns a populated MediaDB.
func LoadDb() (MediaDB, error) {
	var mdb MediaDB
	dbfile, err := ExpandTilde(*Dbfile)
	if err != nil {
		return mdb, err
	}
	f, err := os.Open(dbfile)
	if err != nil {
		return mdb, err
	}
	defer f.Close()
	return mdb, json.NewDecoder(f).Decode(&mdb)
}

// SaveDb() saves the given MediaDB to the cache file on disk in JSON form.
func SaveDb(mdb MediaDB) error {
	dbfile, err := ExpandTilde(*Dbfile)
	if err != nil {
		return err
	}
	f, err := os.Create(dbfile)
	if err != nil {
		return err
	}
	defer f.Close()
	mdb.compact()
	return json.NewEncoder(f).Encode(mdb)
}

// indexByPath() builds a map[string]int that lets you find MasterFiles in mdb
// by path string.
func indexByPath(mdb *MediaDB) map[string]int {
	idx := make(map[string]int, len(mdb.MasterFiles))
	for i, mf := range mdb.MasterFiles {
		idx[mf.Path] = i
	}
	return idx
}

func Extension(path string) string {
	components := strings.Split(path, ".")
	return components[len(components)-1]
}

// NewMasterFile() is a constructor that initializes a MasterFile
// given a path and its os.FileInfo.  Always returns a MasterFile, but
// with Valid == false if the format is unknown, if the metadata
// parser failed, or the attempt to open it failed.
func NewMasterFile(path string, info os.FileInfo) *MasterFile {
	// initialize a MasterFile with the basic information from stat
	mf := new(MasterFile)
	mf.Path = path
	mf.Mtime = info.ModTime()

	// perform any format-specific inspection for metadata
	handler, ok := HandlerByExt[Extension(info.Name())]
	if !ok {
		// we have no handler for this file type; ignore it
		return mf
	}

	if err := handler(mf); err != nil {
		return mf
	}
	// NB, handler() is expected to have set Valid == true if it worked.

	fd, err := os.Open(path)
	if err != nil {
		mf.Valid = false
	}
	defer fd.Close()

	return mf
}

// ScanFiles() walks the file tree from mdb.FileRoot, and updates mdb to match
// whatever it finds.  The result is the same whether you are "updating" an
// existing MediaDB or an empty one.  The only difference is the messages
// printed to "msgs": if you started empty, every file is reported as "new."
func ScanFiles(mdb *MediaDB, msgs io.Writer) error {

	pathIndex := indexByPath(mdb)

	for i := range mdb.MasterFiles {
		mdb.MasterFiles[i].Valid = false
	}

	err := filepath.Walk(mdb.FileRoot,
		func(path string, info os.FileInfo, err error) error {

			if err != nil {
				fmt.Fprintf(msgs, "fatal error scanning %v: %v\n", path, err)
				return err
			}

			if info.IsDir() {
				return nil
			}

			i, ok := pathIndex[path]
			if ok && info.ModTime() == mdb.MasterFiles[i].Mtime {
				mdb.MasterFiles[i].Valid = true
				return nil
			}

			mf := NewMasterFile(path, info)
			if mf == nil {
				return nil
			} else if mf.Valid == false {
				fmt.Fprintf(msgs, "invalid file: %v\n", path)
				return nil
			}

			if ok {
				fmt.Fprintf(msgs, "changed file: %v\n", path)
				mdb.MasterFiles[i] = *mf
			} else {
				fmt.Fprintf(msgs, "new file: %v\n", path)
				mf.Valid = true
				mdb.MasterFiles = append(mdb.MasterFiles, *mf)
			}

			return nil
		})

	if err != nil {
		return err
	}

	for i := range mdb.MasterFiles {
		if !mdb.MasterFiles[i].Valid {
			fmt.Fprintf(msgs, "deleted file: %v\n", mdb.MasterFiles[i].Path)
		}
	}
	mdb.compact()

	return nil
}

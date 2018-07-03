/*
Functions for manipulating and persisting the database of master media files.

Outside of this package, code will mostly want to interact with a MediaDB
object.  It consists of the file root (so that it can be re-scanned without
specifying it again) and a list of MasterFile objects.
*/

package db

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/meta"
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
func indexByPath(mdb MediaDB) map[string]int {
	idx := make(map[string]int, len(mdb.MasterFiles))
	for i, mf := range mdb.MasterFiles {
		idx[mf.Path] = i
	}
	return idx
}

// NewMasterFile() is a constructor that initializes a MasterFile given a path
// and its os.FileInfo.  Returns nil if the given file name isn't recognized
// as a media type.  Returns a MasterFile with Valid already set to false if
// it was not possible to open the file or parse metadata.
func NewMasterFile(path string, info os.FileInfo) *MasterFile {
	mf := new(MasterFile)
	mf.Path = path
	mf.Mtime = info.ModTime()

	components := strings.Split(info.Name(), ".")
	switch components[len(components)-1] {
	case "mp4":
		mf.Type = Video
		if err := inspectMpeg(mf); err != nil {
			return mf
		}
	case "mp3":
		mf.Type = Audio
	case "flac":
		mf.Type = Audio
		if err := inspectFlac(mf); err != nil {
			return mf
		}
	default:
		return nil
	}

	fd, err := os.Open(path)
	if err != nil {
		mf.Valid = false
	}
	defer fd.Close()

	// NB that if we return an object where Valid == false, it will be discarded
	// from the library immediately.
	return mf
}

// ScanFiles() walks the file tree from mdb.FileRoot, and updates mdb to match
// whatever it finds.  The result is the same whether you are "updating" an
// existing MediaDB or an empty one.  The only difference is the messages
// printed to "msgs": if you started empty, every file is reported as "new."
func ScanFiles(mdb MediaDB, msgs io.Writer) (MediaDB, error) {

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
		return mdb, err
	}

	for i := range mdb.MasterFiles {
		if !mdb.MasterFiles[i].Valid {
			fmt.Fprintf(msgs, "deleted file: %v\n", mdb.MasterFiles[i].Path)
		}
	}
	mdb.compact()

	return mdb, nil
}

// inspectFlac() extracts FLAC metadata from a master file.
func inspectFlac(mf *MasterFile) error {

	stream, err := flac.ParseFile(mf.Path)
	if err != nil {
		fmt.Printf("flac parsing error: %v\n", err)
		return err
	}

	for _, block := range stream.Blocks {
		switch b := block.Body.(type) {
		case *meta.VorbisComment:
			for _, tag := range b.Tags {
				key, val := tag[0], tag[1]
				// discard any [N] junk that is nonstandard
				key = strings.Split(key, "[")[0]
				switch key {
				case "ARTIST":
					mf.Artist = val
				case "ALBUM":
					mf.Album = val
				case "DATE":
					mf.Date = strings.Split(val, "-")[0]
				case "TITLE":
					mf.Title = append(mf.Title, val)
				}
			}
		case *meta.Picture:
			mf.HasPicture = true
		}
	}

	if len(mf.Title) > 0 && len(mf.Title[0]) > 0 {
		mf.Valid = true
	}

	return nil
}

func inspectMpeg(mf *MasterFile) error {

	streamRegex := regexp.MustCompile(
		`Stream #0.(\d+)\((\w+)\): (Audio|Video|Subtitle).*?(\d+) kb/s`)
	metadataRegex := regexp.MustCompile(
		`(title|show|episode_id|date) +: (.*)`)

	cmd := exec.Command("ffprobe", mf.Path)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(stderr)

	if err := cmd.Start(); err != nil {
		fmt.Printf("failed to run ffprobe %v\n", mf.Path)
		fmt.Println(err)
		return err
	}

	for scanner.Scan() {
		line := scanner.Text()
		if m := streamRegex.FindStringSubmatch(line); m != nil {
			// verify that stream number matches what we expect
			index, err := strconv.Atoi(m[1])
			if err != nil || index != len(mf.Stream) {
				panic("unpossible or out-of-order stream number!")
			}

			// parse stream type
			stype, ok := map[string]MediaType{
				"Audio":    Audio,
				"Video":    Video,
				"Subtitle": Subtitle,
			}[m[3]]
			if !ok {
				// you should never get here, because the regex should have only
				// selected a string present in the map.
				panic(fmt.Sprintf("unpossible stream type %v!", m[3]))
			}

			// parse bitrate
			bitrate, err := strconv.Atoi(m[4])
			if err != nil {
				panic(fmt.Sprintf("unpossible bitrate %v!", m[4]))
			}

			sd := MpegStreamDesc{stype, m[2], bitrate}
			mf.Stream = append(mf.Stream, sd)

		} else if m := metadataRegex.FindStringSubmatch(line); m != nil {
			switch m[1] {
			case "title":
				mf.Title = append(mf.Title, m[2])
			case "show":
				mf.Show = m[2]
			case "episode_id":
				mf.Episode = m[2]
			case "date":
				mf.Date = m[2]
			default:
				panic("unpossible metadata tag!")
			}
		}
	}

	if len(mf.Title) > 0 && len(mf.Title[0]) > 0 && len(mf.Stream) > 0 {
		mf.Valid = true
	}

	return cmd.Wait()
}

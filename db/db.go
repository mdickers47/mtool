/*
Functions for manipulating and persisting the database of master media files.

Outside of this package, code will mostly want to interact with a MediaDB
object.  It consists of the file root (so that it can be re-scanned without
specifying it again) and a list of MediaFile objects.
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
	"strings"
	"time"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/meta"
)

var Dbfile = flag.String("dbfile", "~/.mtooldb", "JSON cache file")

// MediaFile represents one master file, which is the preimage to one or more
// derivative files.  If it is ever possible for a MediaFile to be modified
// by this code, it is a bug.

type MediaFile struct {
	Path   string
	Type   string
	Artist string
	Album  string
	Track  []string
	Year   string
	Mtime  time.Time
	Valid  bool // used for transient mark-and-sweep garbage collection
}

// a MediaDB is just a list of MediaFiles, plus we save the FileRoot so that
// you can re-walk the directory without specifying it again.

type MediaDB struct {
	FileRoot   string
	MediaFiles []MediaFile
}

// expandTilde() replaces the first ~ in path with the current user's home
// directory.

func expandTilde(path string) (string, error) {
	usr, err := user.Current()
	if err != nil {
		return path, err
	}
	return strings.Replace(path, "~", usr.HomeDir, 1), nil
}

// compactDb() deletes all of the MediaFiles in mdb where Valid == false.
func compactDb(mdb *MediaDB) {
	newlist := make([]MediaFile, 0, len(mdb.MediaFiles))
	for _, mf := range mdb.MediaFiles {
		if mf.Valid {
			newlist = append(newlist, mf)
		} else {
		}
	}
	mdb.MediaFiles = newlist
	return
}

// LoadDb() parses the cache file on disk and returns a populated MediaDB.
func LoadDb() (MediaDB, error) {
	var mdb MediaDB
	dbfile, err := expandTilde(*Dbfile)
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
	dbfile, err := expandTilde(*Dbfile)
	if err != nil {
		return err
	}
	f, err := os.Create(dbfile)
	if err != nil {
		return err
	}
	defer f.Close()
	compactDb(&mdb)
	return json.NewEncoder(f).Encode(mdb)
}

// indexByPath() builds a map[string]int that lets you find MediaFiles in mdb
// by path string.
func indexByPath(mdb MediaDB) map[string]int {
	idx := make(map[string]int, len(mdb.MediaFiles))
	for i, mf := range mdb.MediaFiles {
		idx[mf.Path] = i
	}
	return idx
}

// NewMediaFile() is a constructor that initializes a MediaFile given a path
// and its os.FileInfo.  Returns nil if the given file name isn't recognized
// as a media type.  Returns a MediaFile with Valid already set to false if
// it was not possible to open the file or parse metadata.

func NewMediaFile(path string, info os.FileInfo) *MediaFile {
	mf := new(MediaFile)
	mf.Path = path
	mf.Mtime = info.ModTime()

	components := strings.Split(info.Name(), ".")
	switch components[len(components)-1] {
	case "mp4":
		mf.Type = "video"
	case "mp3":
		mf.Type = "audio"
	case "flac":
		mf.Type = "flac"
		err := inspectFlac(mf)
		if err != nil {
			return mf
		}
	default:
		return nil
	}

	fd, err := os.Open(path)
	if err != nil {
		return mf
	}
	defer fd.Close()

	defer func() {
		if err := recover(); err != nil {
			fmt.Printf("tag library panic: %v\n", err)
			fmt.Printf("the bad file is: %v\n", path)
		}
		return
	}()

	/*
	md, err := tag.ReadFrom(fd)
	if err != nil {
		return mf
	}
	mf.Type = md.FileType()
	mf.Title = md.Title()
	mf.Artist = md.Artist()
	mf.Year = md.Year()
	mf.Track, _ = md.Track()
   */
	
	// NB that if we fell out of this function anywhere before getting to
	// Valid = true, then it will be discarded from the library immediately.
	mf.Valid = true

	return mf
}

// ScanFiles() walks the file tree from mdb.FileRoot, and updates mdb to match
// whatever it finds.  The result is the same whether you are "updating" an
// existing MediaDB or an empty one.  The only difference is the messages
// printed to "msgs": if you started empty, every file is reported as "new."
func ScanFiles(mdb MediaDB, msgs io.Writer) (MediaDB, error) {

	pathIndex := indexByPath(mdb)

	for i := range mdb.MediaFiles {
		mdb.MediaFiles[i].Valid = false
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
			if ok && info.ModTime() == mdb.MediaFiles[i].Mtime {
				mdb.MediaFiles[i].Valid = true
				return nil
			}

			mf := NewMediaFile(path, info)
			if mf == nil {
				return nil
			} else if mf.Valid == false {
				fmt.Fprintf(msgs, "invalid file: %v\n", path)
				return nil
			}

			if ok {
				fmt.Fprintf(msgs, "changed file: %v\n", path)
				mdb.MediaFiles[i] = *mf
			} else {
				fmt.Fprintf(msgs, "new file: %v\n", path)
				mf.Valid = true
				mdb.MediaFiles = append(mdb.MediaFiles, *mf)
			}

			return nil
		})

	if err != nil {
		return mdb, err
	}

	for i := range mdb.MediaFiles {
		if !mdb.MediaFiles[i].Valid {
			fmt.Fprintf(msgs, "deleted file: %v\n", mdb.MediaFiles[i].Path)
		}
	}
	compactDb(&mdb)

	return mdb, nil
}

func inspectFlac(mf *MediaFile) error {

	stream, err := flac.ParseFile(mf.Path)
	if err != nil {
		fmt.Printf("flac parsing error: %v\n", err)
		return err
	}

	for _, block := range stream.Blocks {
		vc, ok := block.Body.(*meta.VorbisComment)
		if ok {
			for _, tag := range vc.Tags {
				key, val := tag[0], tag[1]
				// discard any [N] junk that is nonstandard
				key = strings.Split(key, "[")[0]
				switch key {
				case "ARTIST":
					mf.Artist = val
				case "ALBUM":
					mf.Album = val
				case "DATE":
					mf.Year = strings.Split(val, "-")[0]
				case "TITLE":
					mf.Track = append(mf.Track, val)
				}
			}
		}
	}
	
	return nil
}

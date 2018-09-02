package db

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/dhowden/tag"
)

// trim() drops any leading or trailing garbage that is either Unicode
// "white space" or byte 0x00.  You get a ton of this; apparently there
// have been a lot of crappy tag editors in history.
func trim(instr string) string {

	return strings.TrimFunc(instr,
		func(r rune) bool { return unicode.IsSpace(r) || r == 0 })

}

func inspectMp3(mf *MasterFile) error {

	mf.Type = Audio

	f, err := os.Open(mf.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("invalid file %v crashed tag library\n", mf.Path)
			fmt.Printf("panic was: %v\n", r)
		}
	}()

	md, err := tag.ReadFrom(f)
	if err != nil {
		fmt.Printf("failed to read tags: %v\n", err)
		return err
	}

	mf.Title = []string{trim(md.Title())}
	mf.Date = fmt.Sprintf("%v", md.Year())
	mf.HasPicture = (md.Picture() != nil)
	mf.Artist = trim(md.Artist())
	mf.Album = trim(md.Album())
	mf.TrackNum, mf.TrackMax = md.Track()
	mf.Valid = true

	return nil
}

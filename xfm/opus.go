package xfm

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/mdickers47/mtool/db"
)

func pathSafe(instr string) string {

	nerf := func(r rune) rune {
		switch r {
		case '?', '*', '"', '\'', '!', '<', '>', '(', ')':
			return -1 // this means 'delete' to strings.Map()
		case '/', '\\', ':', '#':
			return '-'
		default:
			return r
		}
	}

	outstr := strings.Map(nerf, instr)
	if len(outstr) == 0 {
		outstr = "null"
	}
	return outstr

}

func ImageOpus(mfs []db.MasterFile) []db.ImageFile {

	imfs := make([]db.ImageFile, 0, 100)
	for _, mf := range mfs {
		if mf.Type != db.Audio {
			continue
		}

		for i := 0; i < len(mf.Title); i++ {
			var imf db.ImageFile
			imf.ImagePath = fmt.Sprintf("%v/%v/%02d %v.opus",
				pathSafe(mf.Artist), pathSafe(mf.Album), i+1, pathSafe(mf.Title[i]))
			imf.MasterPath = mf.Path
			imf.MasterMtime = mf.Mtime
			imf.Artist = mf.Artist
			imf.Title = mf.Title[i]
			imf.Album = mf.Album
			imf.Date = mf.Date
			imf.Track = i + 1
			imf.HasPicture = mf.HasPicture
			imfs = append(imfs, imf)
		}
	}
	return imfs
}

func MakeOpus(imf db.ImageFile) error {

	flacargs := []string{
		"--silent",
		"--decode",
		"--stdout",
		fmt.Sprintf("--cue=%v.1-%v.1", imf.Track, imf.Track+1),
		imf.MasterPath}
	opusargs := []string{
		"--quiet",
		"--artist", imf.Artist,
		"--album", imf.Album,
		"--title", imf.Title,
		"--comment", fmt.Sprintf("TRACKNUMBER=%v", imf.Track),
		"--padding", "0"}

	// extract and inject cover image, if any.
	if imf.HasPicture {
		tmpfile, err := ioutil.TempFile("", "mtool")
		if err != nil {
			return err
		}
		defer os.Remove(tmpfile.Name())
		if err := tmpfile.Close(); err != nil {
			return err
		}
		cmd := exec.Command("metaflac",
			"--extract-picture-to", tmpfile.Name(),
			imf.MasterPath)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("crashed running metaflac: %v", err)
		}
		opusargs = append(opusargs, "--picture", tmpfile.Name())
	}

	opusargs = append(opusargs, "-", imf.ImagePath)

	// create path for file to land (or get "exit 1")
	err := os.MkdirAll(path.Dir(imf.ImagePath), 0755)
	if err != nil {
		return fmt.Errorf("failed to create path %v: %v",
			path.Dir(imf.ImagePath), err)
	}

	// hook up pipeline
	flaccmd := exec.Command("flac", flacargs...)
	opuscmd := exec.Command("opusenc", opusargs...)
	if opuscmd.Stdin, err = flaccmd.StdoutPipe(); err != nil {
		return err
	}

	// make it go
	if err := flaccmd.Start(); err != nil {
		fmt.Printf("flac %v\n", flacargs)
		return fmt.Errorf("crashed starting flac: %v", err)
	}
	if err := opuscmd.Run(); err != nil {
		fmt.Printf("opusenc %v\n", opusargs)
		return fmt.Errorf("crashed running opus: %v", err)
	}
	if err := flaccmd.Wait(); err != nil {
		fmt.Printf("flac %v\n", flacargs)
		return fmt.Errorf("crashed waiting for flac: %v", err)
	}

	fmt.Printf("created: %v\n", imf.ImagePath)
	return nil
}

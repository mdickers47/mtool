package xfm

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"

	"github.com/mdickers47/mtool/pkg/db"
)

func ImageAac(mfs []db.MasterFile) []db.ImageFile {

	// identical to the opus imager, but files are named 'm4a'
	imfs := make([]db.ImageFile, 0, 100)
	for _, mf := range mfs {
		if mf.Type != db.Audio {
			continue
		}

		for i := 0; i < len(mf.Title); i++ {
			var imf db.ImageFile
			imf.MasterPath = mf.Path
			imf.MasterMtime = mf.Mtime
			imf.Artist = mf.Artist
			imf.Title = mf.Title[i]
			imf.Album = mf.Album
			imf.Date = mf.Date
			if mf.TrackNum > 0 {
				imf.Track = mf.TrackNum
			} else {
				imf.Track = i + 1
			}
			imf.TrackMax = mf.TrackMax
			imf.HasPicture = mf.HasPicture
			imf.ImagePath = fmt.Sprintf("%v/%v/%02d %.32s.m4a",
				pathSafe(imf.Artist), pathSafe(imf.Album), imf.Track,
				pathSafe(mf.Title[i]))
			imfs = append(imfs, imf)
		}
	}
	return imfs
}

func MakeAac(imf db.ImageFile) error {

	var flacargs []string

	if db.Extension(imf.MasterPath) == "flac" {
		flacargs = []string{
			"flac",
			"--silent",
			"--decode",
			"--stdout",
			fmt.Sprintf("--cue=%v.1-%v.1", imf.Track, imf.Track+1),
			imf.MasterPath}
	} else {
		// flacargs is misnamed in any other case .. oh well.
		flacargs = []string{
			"ffmpeg",
			"-i", imf.MasterPath,
			"-f", "wav",
			"pipe:",
		}
	}

	var trackarg string
	if imf.TrackMax > 0 {
		trackarg = fmt.Sprintf("%v/%v", imf.Track, imf.TrackMax)
	} else {
		trackarg = strconv.Itoa(imf.Track)
	}
	aacargs := []string{
		"fdkaac",
		"--silent",
		"-b128",
		"--artist", imf.Artist,
		"--album", imf.Album,
		"--title", imf.Title,
		"--track", trackarg,
		"-o", imf.ImagePath,
		"-"} // input file is stdin
	//"--comment", fmt.Sprintf("TRACKNUMBER=%v", imf.Track),
	//"--padding", "0"}

	// TODO: Don't know how to place cover art in the m4a container
	// except by using the Nero aac tool that I don't want to deal
	// with.

	// create path for file to land (or get "exit 1")
	err := os.MkdirAll(path.Dir(imf.ImagePath), 0755)
	if err != nil {
		return err
	}

	// hook up pipeline
	flaccmd := exec.Command(flacargs[0], flacargs[1:]...)
	aaccmd := exec.Command(aacargs[0], aacargs[1:]...)
	if aaccmd.Stdin, err = flaccmd.StdoutPipe(); err != nil {
		return err
	}

	// make it go
	if err := flaccmd.Start(); err != nil {
		fmt.Printf("flac %v\n", flacargs)
		return fmt.Errorf("crashed starting flac: %v", err)
	}
	if err := aaccmd.Run(); err != nil {
		fmt.Printf("fdkaac %v\n", aacargs)
		return fmt.Errorf("crashed running fdkaac: %v", err)
	}
	if err := flaccmd.Wait(); err != nil {
		fmt.Printf("flac %v\n", flacargs)
		return fmt.Errorf("crashed waiting for flac: %v", err)
	}

	fmt.Printf("created: %v\n", imf.ImagePath)
	return nil
}

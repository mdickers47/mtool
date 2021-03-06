package xfm

import (
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/mdickers47/mtool/pkg/db"
)

func ImageOpus(mfs []db.MasterFile) []db.ImageFile {

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
			imf.HasPicture = mf.HasPicture
			imf.ImagePath = fmt.Sprintf("%v/%v/%02d %.32v.opus",
				pathSafe(imf.Artist), pathSafe(imf.Album), imf.Track,
				pathSafe(mf.Title[i]))
			imfs = append(imfs, imf)
		}
	}
	return imfs
}

func MakeOpus(imf db.ImageFile) error {

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
	opusargs := []string{
		"opusenc",
		"--quiet",
		"--artist", imf.Artist,
		"--album", imf.Album,
		"--title", imf.Title,
		"--date", imf.Date,
		"--tracknumber", fmt.Sprintf("%v", imf.Track),
		"--padding", "0"}

	// extract and inject cover image, if any.
	if imf.HasPicture {
		picfile, err := getPicture(imf.MasterPath)
		if err != nil {
			return fmt.Errorf("failed to extract cover art: %v", err)
		}
		defer os.Remove(picfile)
		opusargs = append(opusargs, "--picture", picfile)
	}

	opusargs = append(opusargs, "-", imf.ImagePath)

	// create path for file to land (or get "exit 1")
	err := os.MkdirAll(path.Dir(imf.ImagePath), 0755)
	if err != nil {
		return err
		//return fmt.Errorf("failed to create path %v: %v",
		//	path.Dir(imf.ImagePath), err)
	}

	// hook up pipeline
	flaccmd := exec.Command(flacargs[0], flacargs[1:]...)
	opuscmd := exec.Command(opusargs[0], opusargs[1:]...)
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

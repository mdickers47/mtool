package xfm

import (
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/mdickers47/mtool/pkg/db"
)

func ImageMp3(mfs []db.MasterFile) []db.ImageFile {

	imfs := make([]db.ImageFile, 0, 100)
	for _, mf := range mfs {
		if mf.Type != db.Audio {
			continue
		}

		for i := 0; i < len(mf.Title); i++ {
			var imf db.ImageFile
			imf.MasterPath = mf.Path
			imf.MasterMtime = mf.Mtime
			imf.AlbumArtist = mf.Artist
			imf.Album = mf.Album
			imf.Date = mf.Date
			imf.Artist, imf.Title, imf.Track = mf.GetTrackTags(i)
			imf.HasPicture = mf.HasPicture
			imf.ImagePath = fmt.Sprintf("%v/%v/%02d %.32v.mp3",
				pathSafe(imf.AlbumArtist), pathSafe(imf.Album), imf.Track,
				pathSafe(imf.Title))
			imfs = append(imfs, imf)
		}
	}
	return imfs
}

func MakeMp3(imf db.ImageFile) error {

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
	mp3args := []string{
		"lame",
		"--preset", "standard",
		"--quiet",
		"--ta", imf.Artist,
		"--tl", imf.Album,
		"--tt", imf.Title,
		"--ty", imf.Date, // must be year only, 1 to 9999
		"--tn", fmt.Sprintf("%v", imf.Track),
	}

	// extract and inject cover image, if any.
	if imf.HasPicture {
		picfile, err := getPicture(imf.MasterPath)
		if err != nil {
			return fmt.Errorf("failed to extract cover art: %v", err)
		}
		defer os.Remove(picfile)
		mp3args = append(mp3args, "--ti", picfile)
	}

	// input file and output file
	mp3args = append(mp3args, "-", imf.ImagePath)

	// create path for file to land (or get "exit 1")
	err := os.MkdirAll(path.Dir(imf.ImagePath), 0755)
	if err != nil {
		return err
	}

	// hook up pipeline
	flaccmd := exec.Command(flacargs[0], flacargs[1:]...)
	mp3cmd := exec.Command(mp3args[0], mp3args[1:]...)
	if mp3cmd.Stdin, err = flaccmd.StdoutPipe(); err != nil {
		return err
	}

	// make it go
	if err := flaccmd.Start(); err != nil {
		fmt.Printf("flac: %v\n", flacargs)
		return fmt.Errorf("crashed starting flac: %v", err)
	}
	if err := mp3cmd.Run(); err != nil {
		fmt.Printf("lame: %v\n", mp3args)
		return fmt.Errorf("crashed running lame: %v", err)
	}
	if err := flaccmd.Wait(); err != nil {
		fmt.Printf("flac: %v\n", mp3args)
		return fmt.Errorf("crashed waiting for flac: %v", err)
	}

	fmt.Printf("created: %v\n", imf.ImagePath)
	return nil
}

package db

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
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

func ImageOpus(mfs []MasterFile) []ImageFile {

	imfs := make([]ImageFile, 0, 100)
	for _, mf := range mfs {
		if mf.Type != Audio {
			continue
		}

		for i := 0; i < len(mf.Title); i++ {
			var imf ImageFile
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

func MakeOpus(imf ImageFile) error {

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

func ImageWebm(mfs []MasterFile) []ImageFile {

	imfs := make([]ImageFile, 0, 100)
	for _, mf := range mfs {
		if mf.Type != Video {
			continue
		}
		var imf ImageFile
		if len(mf.Show) > 0 {
			imf.ImagePath = fmt.Sprintf("tv/%v/%v %v.mkv",
				pathSafe(mf.Show), pathSafe(mf.Episode), pathSafe(mf.Title[0]))
		} else {
			imf.ImagePath = fmt.Sprintf("movies/%v/%v.mkv",
				pathSafe(mf.Date), pathSafe(mf.Title[0]))
		}
		imf.MasterPath = mf.Path
		imf.MasterMtime = mf.Mtime
		imf.Date = mf.Date
		imf.Title = mf.Title[0]
		imf.Stream = mf.Stream
		// these probably aren't present and won't be used, but what the hell
		imf.Artist = mf.Artist
		imf.Album = mf.Album

		imfs = append(imfs, imf)
	}

	return imfs
}

var Language = flag.String("language", "eng",
	"which streams to extract from master files")

func MakeWebm(imf ImageFile) error {

	var mapArgs []string

	// we keep the first video stream; typically there is only one
	for i, sd := range imf.Stream {
		if sd.Type == Video {
			mapArgs = append(mapArgs, "-map", fmt.Sprintf("0:%v", i))
			break
		}
	}

	// we are going to keep one audio stream, but choosing is complicated.
	// We sort the audio streams as follows:
	//
	// language "eng" ahead of language != "eng"
	// language "und" ahead of language != "und"
	// higher bitrate ahead of lower bitrate
	//
	// Then take the first one.  The value of "eng" can be changed via
	// flag.  Language "und" is preferred over tracks correcly tagged
	// with a non-"eng" language, because some DVDs leave the primary
	// track unmarked, even when alternate audio tracks are correct.

	type EnumStream struct {
		Index  int
		Stream MpegStreamDesc
	}

	streams := make([]EnumStream, 0, len(imf.Stream))
	for i, sd := range imf.Stream {
		if sd.Type == Audio {
			streams = append(streams, EnumStream{i, sd})
		}
	}

	kookySort := func(i, j int) bool {
		lang_i := streams[i].Stream.Language
		lang_j := streams[j].Stream.Language
		switch {
		case lang_i == *Language && lang_j != *Language:
			return true
		case lang_i != *Language && lang_j == *Language:
			return false
		case lang_i == "und" && lang_j != "und":
			return true
		case lang_i != "und" && lang_j == "und":
			return false
		default:
			return streams[i].Stream.Bitrate > streams[j].Stream.Bitrate
		}
	}

	if len(streams) == 0 {
		fmt.Printf("warning: no audio streams in %v", imf.MasterPath)
	} else {
		sort.SliceStable(streams, kookySort)
		fmt.Printf("sorted streams: %v\n", streams)
		mapArgs = append(mapArgs, "-map", fmt.Sprintf("0:%v", streams[0].Index))
	}

	// we keep all subtitle streams in $language.  They have to be
	// repacked using the same dvd_subtitle codec, because "copy" craps
	// out when moving from an MPEG master to a Matroska container.
	for i, sd := range imf.Stream {
		if sd.Type == Subtitle && sd.Language == *Language {
			mapArgs = append(mapArgs, "-map", fmt.Sprintf("0:%v", i))
		}
	}

	args := []string{
		"-probesize", "200M",
		"-analyzeduration", "120M",
		"-i", imf.MasterPath,
	}
	args = append(args, mapArgs...)
	// arguments that control video codec
	args = append(args, "-c:v", "libvpx-vp9", "-crf", "33", "-b:v", "0")
	// arguments that control audio codec
	args = append(args, "-c:a", "libopus", "-b:a", "192000")
	// arguments that control subtitle codec
	args = append(args, "-c:s", "dvd_subtitle")
	// output file
	args = append(args, imf.ImagePath)

	err := os.MkdirAll(path.Dir(imf.ImagePath), 0755)
	if err != nil {
		return fmt.Errorf("failed to create path %v: %v",
			path.Dir(imf.ImagePath), err)
	}

	cmd := exec.Command("ffmpeg", args...)
	//cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("can't run: %v", err)
	}

	fmt.Printf("created: %v\n", imf.ImagePath)
	return nil

}

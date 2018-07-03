package xfm

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"

	"github.com/mdickers47/mtool/db"
)

// Language is a flag that tells video encoders which audio and subtitle
// streams you prefer to keep.

var Language = flag.String("language", "eng",
	"which streams to extract from master files")

func ImageWebm(mfs []db.MasterFile) []db.ImageFile {

	imfs := make([]db.ImageFile, 0, 100)
	for _, mf := range mfs {
		if mf.Type != db.Video {
			continue
		}
		var imf db.ImageFile
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

func MakeWebm(imf db.ImageFile) error {

	var mapArgs []string

	// we keep the first video stream; typically there is only one
	for i, sd := range imf.Stream {
		if sd.Type == db.Video {
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
		Stream db.MpegStreamDesc
	}

	streams := make([]EnumStream, 0, len(imf.Stream))
	for i, sd := range imf.Stream {
		if sd.Type == db.Audio {
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
		if sd.Type == db.Subtitle && sd.Language == *Language {
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

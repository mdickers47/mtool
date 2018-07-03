package db

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

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

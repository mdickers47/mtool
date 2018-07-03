package db

import (
	"fmt"
	"strings"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/meta"
)

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

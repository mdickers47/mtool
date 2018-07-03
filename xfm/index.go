package xfm

import (
	"github.com/mdickers47/mtool/db"
)

type Transformer struct {
	Image func([]db.MasterFile) []db.ImageFile
	Make  func(db.ImageFile) error
}

var Byname = map[string]Transformer{
	"opus": Transformer{ImageOpus, MakeOpus},
	"webm": Transformer{ImageWebm, MakeWebm},
}

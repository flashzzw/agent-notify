package common

import (
	"encoding/base64"
	"encoding/binary"
	"unicode/utf16"
)

// EncodePowerShell encodes a script for Windows PowerShell's -EncodedCommand.
func EncodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	encoded := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		encoded = binary.LittleEndian.AppendUint16(encoded, unit)
	}
	return base64.StdEncoding.EncodeToString(encoded)
}

package navidrome

import (
	"bytes"
	"cmp"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/rairulyle/bitchord-selfhosted-addon/internal/media"
)

// ID is a Subsonic id. Navidrome sends music folder ids as numbers and
// everything else as strings, so it accepts either.
type ID string

func (i *ID) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(raw, []byte("null")) {
		*i = ""
		return nil
	}
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		*i = ID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return err
	}
	*i = ID(n.String())
	return nil
}

type Folder struct {
	ID   ID     `json:"id"`
	Name string `json:"name"`
}

// Song is the part of a Subsonic child the addon reads. The OpenSubsonic
// fields are zero on servers that do not send them.
type Song struct {
	ID                 string `json:"id"`
	IsDir              bool   `json:"isDir"`
	IsVideo            bool   `json:"isVideo"`
	Title              string `json:"title"`
	Album              string `json:"album"`
	Artist             string `json:"artist"`
	DisplayArtist      string `json:"displayArtist"`
	DisplayAlbumArtist string `json:"displayAlbumArtist"`
	CoverArt           string `json:"coverArt"`
	Suffix             string `json:"suffix"`
	ContentType        string `json:"contentType"`
	Duration           int    `json:"duration"`
	BitRate            int    `json:"bitRate"`
	SamplingRate       int    `json:"samplingRate"`
	BitDepth           int    `json:"bitDepth"`
}

func (s Song) audio() bool { return s.ID != "" && !s.IsDir && !s.IsVideo }

func (s Song) convert() media.Track {
	codec, container := formatOf(s.Suffix, s.ContentType, s.BitDepth)
	return media.Track{
		ID:          s.ID,
		Title:       s.Title,
		Artist:      cmp.Or(s.DisplayArtist, s.Artist, s.DisplayAlbumArtist),
		AlbumArtist: s.DisplayAlbumArtist,
		Album:       s.Album,
		DurationSec: s.Duration,
		Codec:       media.AudioFormat(codec, container),
		Container:   container,
		BitrateKbps: s.BitRate,
		SampleRate:  s.SamplingRate,
		BitDepth:    s.BitDepth,
		FileRef:     FilePath(s.ID),
		ArtRef:      artRef(s.CoverArt),
	}
}

// formatOf guesses the codec from the file suffix, since Subsonic does not
// name it. An MP4 file with a bit depth is ALAC: AAC has none.
func formatOf(suffix, contentType string, bitDepth int) (codec, container string) {
	suffix = strings.ToLower(strings.TrimPrefix(suffix, "."))
	switch suffix {
	case "m4a", "m4b", "mp4":
		if bitDepth > 0 {
			return "alac", "mp4"
		}
		return "aac", "mp4"
	case "alac":
		return "alac", "mp4"
	case "ogg", "oga":
		if strings.Contains(strings.ToLower(contentType), "opus") {
			return "opus", "ogg"
		}
		return "vorbis", "ogg"
	case "opus":
		return "opus", "ogg"
	case "wav":
		return "pcm", "wav"
	case "aif", "aiff", "aifc":
		return "pcm", "aiff"
	case "wv":
		return "wavpack", "wv"
	case "dsf", "dff":
		return "dsd", suffix
	}
	return suffix, suffix
}

func artRef(coverArt string) string {
	if coverArt == "" {
		return ""
	}
	return ArtPath(coverArt)
}

// FilePath asks for the original file: format=raw turns off any transcoding
// the server would otherwise apply for this client.
func FilePath(id string) string {
	return "/rest/stream?" + url.Values{"id": {id}, "format": {"raw"}}.Encode()
}

func ArtPath(coverArt string) string {
	return "/rest/getCoverArt?" + url.Values{"id": {coverArt}, "size": {"600"}}.Encode()
}

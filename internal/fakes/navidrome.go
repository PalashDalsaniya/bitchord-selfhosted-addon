package fakes

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rairulyle/bitchord-selfhosted-addon/internal/navidrome"
)

const (
	NavidromeUser     = "palash"
	NavidromePassword = "correct horse"
	NavidromeAPIKey   = "nds_fake-navidrome-key"
	NavidromeVersion  = "0.58.0 (fake)"
	// Navidrome ids are 22 base62 characters.
	NavidromeFlacID = "4kTlOjJpmyY1B9MoGbiPXm"
	NavidromeALACID = "0aBcDeFgHiJkLmNoPqRsTu"
	NavidromeMP3ID  = "1ZyXwVuTsRqPoNmLkJiHgF"
	NavidromeBookID = "2QwErTyUiOpAsDfGhJkLzX"
)

// Navidrome answers the Subsonic calls the adapter makes, in JSON, with a
// failed status and an error code where a real server would.
type Navidrome struct {
	recorder
	URL          string
	Folders      []navidrome.Folder
	Songs        map[string][]navidrome.Song
	Files        map[string][]byte
	IgnorePaging bool
}

func NewNavidrome(t testing.TB) *Navidrome {
	f := &Navidrome{Folders: NavidromeFolders(), Songs: NavidromeSongs(), Files: NavidromeFiles()}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	f.URL = server.URL
	return f
}

func (f *Navidrome) serve(w http.ResponseWriter, r *http.Request) {
	status, rawBody := f.record(r)
	query := r.URL.Query()
	if query.Get("f") != "json" || query.Get("v") == "" || query.Get("c") == "" {
		f.fail(w, 10, "required parameter is missing")
		return
	}
	if code := authenticate(query); code != 0 {
		f.fail(w, code, "wrong username or password")
		return
	}
	if f.override(w, status, rawBody) {
		return
	}
	switch strings.TrimSuffix(r.URL.Path, ".view") {
	case "/rest/ping":
		f.ok(w, nil)
	case "/rest/getMusicFolders":
		f.ok(w, map[string]any{"musicFolders": map[string]any{"musicFolder": f.Folders}})
	case "/rest/search3":
		f.search(w, query)
	case "/rest/getSong":
		for _, songs := range f.Songs {
			for _, song := range songs {
				if song.ID == query.Get("id") {
					f.ok(w, map[string]any{"song": song})
					return
				}
			}
		}
		f.fail(w, 70, "data not found")
	case "/rest/stream":
		f.stream(w, r, query)
	case "/rest/getCoverArt":
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("jpeg:" + query.Get("id") + "@" + query.Get("size")))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// authenticate answers 0 for the API key alone, or for the known user's
// salted token, and a Subsonic error code otherwise.
func authenticate(query url.Values) int {
	if key := query.Get("apiKey"); key != "" {
		for _, conflict := range []string{"u", "p", "t", "s"} {
			if query.Has(conflict) {
				return 43
			}
		}
		if key != NavidromeAPIKey {
			return 44
		}
		return 0
	}
	if query.Has("p") {
		return 40
	}
	sum := md5.Sum([]byte(NavidromePassword + query.Get("s")))
	if query.Get("u") != NavidromeUser || query.Get("s") == "" || query.Get("t") != hex.EncodeToString(sum[:]) {
		return 40
	}
	return 0
}

func (f *Navidrome) ok(w http.ResponseWriter, body map[string]any) {
	answer := map[string]any{"status": "ok", "version": "1.16.1", "type": "navidrome", "serverVersion": NavidromeVersion, "openSubsonic": true}
	for key, value := range body {
		answer[key] = value
	}
	writeJSON(w, map[string]any{"subsonic-response": answer})
}

func (f *Navidrome) fail(w http.ResponseWriter, code int, message string) {
	writeJSON(w, map[string]any{"subsonic-response": map[string]any{
		"status": "failed", "version": "1.16.1", "error": map[string]any{"code": code, "message": message},
	}})
}

func (f *Navidrome) search(w http.ResponseWriter, query url.Values) {
	if query.Get("query") != `""` {
		f.ok(w, map[string]any{"searchResult3": map[string]any{}})
		return
	}
	var all []navidrome.Song
	for _, folder := range f.Folders {
		if folder := string(folder.ID); query.Get("musicFolderId") == "" || query.Get("musicFolderId") == folder {
			all = append(all, f.Songs[folder]...)
		}
	}
	if !f.IgnorePaging {
		offset, _ := strconv.Atoi(query.Get("songOffset"))
		count, err := strconv.Atoi(query.Get("songCount"))
		if err != nil {
			count = 20
		}
		offset = min(offset, len(all))
		all = all[offset:min(offset+count, len(all))]
	}
	f.ok(w, map[string]any{"searchResult3": map[string]any{"song": all}})
}

func (f *Navidrome) stream(w http.ResponseWriter, r *http.Request, query url.Values) {
	body, ok := f.Files[query.Get("id")]
	if !ok {
		f.fail(w, 70, "data not found")
		return
	}
	if query.Get("format") != "raw" {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("transcoded"))
		return
	}
	w.Header().Set("Content-Type", "audio/flac")
	http.ServeContent(w, r, "", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), bytes.NewReader(body))
}

func NavidromeFolders() []navidrome.Folder {
	return []navidrome.Folder{{ID: "1", Name: "Music"}, {ID: "2", Name: "Audiobooks"}}
}

func NavidromeSongs() map[string][]navidrome.Song {
	return map[string][]navidrome.Song{
		"1": {
			{ID: NavidromeFlacID, Title: "New Religion", Album: "Tell Me I’m Alive", Artist: "All Time Low", DisplayArtist: "All Time Low, Teddy Swims", DisplayAlbumArtist: "All Time Low",
				CoverArt: "al-" + NavidromeFlacID, Suffix: "flac", ContentType: "audio/flac", Duration: 184, BitRate: 1875, SamplingRate: 48000, BitDepth: 24},
			{ID: NavidromeALACID, Title: "Closer", Album: "As You Found Me", Artist: "Anberlin", Suffix: "m4a", ContentType: "audio/mp4", Duration: 230, BitRate: 900, SamplingRate: 44100, BitDepth: 16},
			{ID: NavidromeMP3ID, Title: "Endless Slaughter", Album: "Endless Slaughter", Artist: "Limp Bizkit", CoverArt: "mf-" + NavidromeMP3ID, Suffix: "mp3", ContentType: "audio/mpeg", Duration: 337, BitRate: 320, SamplingRate: 44100},
			{ID: "dir-1", IsDir: true, Title: "A Folder"},
		},
		"2": {
			{ID: NavidromeBookID, Title: "Chapter One", Album: "Some Book", Artist: "Some Author", Suffix: "mp3", ContentType: "audio/mpeg", Duration: 1800, BitRate: 64},
		},
	}
}

func NavidromeFiles() map[string][]byte {
	return map[string][]byte{
		NavidromeFlacID: []byte("0123456789abcdefghijklmnopqrstuvwxyz"),
		NavidromeMP3ID:  []byte("mp3-bytes"),
	}
}

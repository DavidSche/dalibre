package app

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"dalibre/cmd/dalibre/locales"
	"dalibre/internal/calibre"
	"dalibre/internal/settings"
)

// koreader answers as KOReader's calibre plugin does, enough to be sent
// books; it keeps the lpaths it got.
type koreader struct {
	conn     net.Conn
	books    []map[string]any // on it before it connects
	password string
	mu       sync.Mutex
	got      []string
	deleted  []string
	updated  []string // titles SEND_BOOK_METADATA brought
	refused  bool     // told its password is wrong
}

func (k *koreader) write(op int, arg any) {
	js, _ := json.Marshal([]any{op, arg})
	k.conn.Write(append([]byte(strconv.Itoa(len(js))), js...))
}

func (k *koreader) serve(br *bufio.Reader) {
	for {
		n := 0
		for {
			b, err := br.ReadByte()
			if err != nil {
				return
			}
			if b == '[' {
				break
			}
			n = n*10 + int(b-'0')
		}
		buf := append([]byte{'['}, make([]byte, n-1)...)
		if _, err := io.ReadFull(br, buf[1:]); err != nil {
			return
		}
		var msg []json.RawMessage
		json.Unmarshal(buf, &msg)
		var op int
		var arg map[string]any
		json.Unmarshal(msg[0], &op)
		json.Unmarshal(msg[1], &arg)
		switch op {
		case 9: // GET_INITIALIZATION_INFO
			hash := ""
			if c, _ := arg["passwordChallenge"].(string); c != "" {
				sum := sha1.Sum([]byte(k.password + c))
				hash = hex.EncodeToString(sum[:])
			}
			k.write(0, map[string]any{"appName": "KOReader", "acceptedExtensions": []string{"epub", "pdf"},
				"canDeleteMultipleBooks": true, "canReceiveBookBinary": true, "canSendOkToSendbook": true,
				"canStreamBooks": true, "canStreamMetadata": true, "canUseCachedMetadata": true,
				"deviceName": "KOReader (Kobo)", "passwordHash": hash, "maxBookContentPacketLen": 4096, "versionOK": true})
		case 3: // GET_DEVICE_INFORMATION
			k.write(0, map[string]any{"device_info": map[string]any{}, "version": "1", "device_version": "1"})
		case 5: // FREE_SPACE
			k.write(0, map[string]any{"free_space_on_device": 1 << 30})
		case 6: // GET_BOOK_COUNT
			k.write(0, map[string]any{"count": len(k.books), "willStream": true})
			for i, b := range k.books {
				k.write(0, map[string]any{"priKey": i + 1, "uuid": b["uuid"], "lpath": b["lpath"], "last_modified": b["last_modified"]})
			}
		case 13: // DELETE_BOOK
			k.write(0, map[string]any{})
			for _, l := range arg["lpaths"].([]any) {
				k.mu.Lock()
				k.deleted = append(k.deleted, l.(string))
				k.mu.Unlock()
				k.write(0, map[string]any{"uuid": ""})
			}
		case 7: // SEND_BOOKLISTS: no answer
		case 16: // SEND_BOOK_METADATA: no answer
			k.mu.Lock()
			k.updated = append(k.updated, arg["data"].(map[string]any)["title"].(string))
			k.mu.Unlock()
		case 17: // DISPLAY_MESSAGE
			k.mu.Lock()
			k.refused = true
			k.mu.Unlock()
		case 8: // SEND_BOOK
			k.write(0, map[string]any{})
			io.ReadFull(br, make([]byte, int(arg["length"].(float64))))
			k.mu.Lock()
			k.got = append(k.got, arg["lpath"].(string))
			k.mu.Unlock()
		default:
			k.write(0, map[string]any{})
		}
	}
}

// queuePosts makes a's posts wait in a queue until pump runs them, on
// the test's goroutine, as the window's main thread would: the
// connection posts from its own goroutines.
func queuePosts(a *App) (pump func()) {
	q := make(chan func(), 1024)
	a.post = func(fn func()) { q <- fn }
	return func() {
		for {
			select {
			case fn := <-q:
				fn()
			default:
				return
			}
		}
	}
}

// connectKOReader connects k to a's wireless connection and waits until
// it is connected, or refused.
func connectKOReader(t *testing.T, a *App, pump func(), k *koreader) {
	t.Helper()
	conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(a.device.srv.Port()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	k.conn = conn
	go k.serve(bufio.NewReader(conn))
	for range 200 {
		pump()
		k.mu.Lock()
		refused := k.refused
		k.mu.Unlock()
		if a.connectedDevice() != nil || refused {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("KOReader didn't connect")
}

func TestKOReaderPasswordAndSync(t *testing.T) {
	a := newTestApp(t)
	a.secrets = memorySecrets{}
	pump := queuePosts(a)
	a.settings.Wireless = settings.Wireless{On: true, Port: freePort(t)}
	a.applyDevice()
	pump()
	defer func() {
		a.settings.Wireless.On = false
		a.applyDevice()
		pump()
	}()

	// Settings › KOReader: a password, applied.
	a.openSettings(sectionWireless)
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if !tt.HasText(locales.WirelessRequirePassword()) || !tt.HasText(a.deviceAddress()) {
		t.Fatalf("Settings › KOReader: %q", tt.Texts())
	}
	shot(t, tt, "settings-koreader")
	a.settings.Wireless.Password = true
	a.secrets.Set(wirelessSecret, "secret")
	a.device.srv.Close()
	a.device = nil
	a.applyDevice()
	pump()
	a.settingsOpen = false

	wrong := &koreader{password: "nope"}
	connectKOReader(t, a, pump, wrong)
	if a.connectedDevice() != nil {
		t.Fatal("KOReader with the wrong password connected")
	}
	if status, bad := a.deviceStatus(); !bad || status != locales.WirelessWrongPassword() {
		t.Fatalf("status %q", status)
	}

	// The right one, with Piranesi on it from before it was renamed.
	var piranesi *calibre.Book
	for _, b := range a.books {
		if b.Title == "Piranesi" {
			piranesi = b
		}
	}
	k := &koreader{password: "secret", books: []map[string]any{
		{"uuid": piranesi.UUID, "lpath": "Clarke, Susanna/Piranesi.epub", "last_modified": "2001-01-01T00:00:00+00:00"},
	}}
	wrong.conn.Close()
	connectKOReader(t, a, pump, k)
	if a.connectedDevice() == nil {
		t.Fatalf("KOReader didn't connect: %v", a.device.srv.Err())
	}
	// Remove waits for answers: the metadata sent on connecting is there.
	a.removeFromDevice(a.onDevice([]int64{piranesi.ID, a.books[0].ID}))
	pump()
	k.mu.Lock()
	updated, deleted := k.updated, k.deleted
	k.mu.Unlock()
	if len(updated) != 1 || updated[0] != "Piranesi" {
		t.Fatalf("metadata sent: %q", updated)
	}
	if len(deleted) != 1 || deleted[0] != "Clarke, Susanna/Piranesi.epub" {
		t.Fatalf("deleted %q", deleted)
	}
	if a.toasts[len(a.toasts)-1].text != "Removed 1 book from KOReader (Kobo)" || a.connectedDevice().Has(piranesi.UUID) {
		t.Fatalf("toasts %+v", a.toasts)
	}
}

func TestSendToKOReader(t *testing.T) {
	a := newTestApp(t)
	pump := queuePosts(a)
	a.settings.Wireless = settings.Wireless{On: true, Port: freePort(t)}
	a.applyDevice()
	pump()
	if a.device == nil || a.device.srv == nil || a.device.err != nil {
		t.Fatalf("device %+v", a.device)
	}
	defer func() {
		a.settings.Wireless.On = false
		a.applyDevice()
		pump()
	}()
	tt := ui.NewTester(a.View, 1440, 900)
	tt.Frame()
	if err := tt.Click(locales.WirelessName()); err != nil {
		t.Fatalf("toolbar: %v; %q", err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText(a.deviceAddress()) || !tt.HasText("Waiting for a reader") {
		t.Fatalf("sharing popover: %q", tt.Texts())
	}

	k := &koreader{}
	connectKOReader(t, a, pump, k)
	tt.Frame()
	if !tt.HasText("KOReader (Kobo) · 0 books") {
		t.Fatalf("connected: %q", tt.Texts())
	}
	shot(t, tt, "sharing-koreader")

	var ids []int64
	for _, b := range a.books {
		if b.Title == "Piranesi" || b.Title == "Dune" {
			ids = append(ids, b.ID)
		}
	}
	a.toasts = nil
	a.sendToDevice(ids)
	pump()
	k.mu.Lock()
	got := k.got
	k.mu.Unlock()
	if len(got) != 2 || !strings.HasSuffix(got[0], ".epub") {
		t.Fatalf("KOReader got %q", got)
	}
	if len(a.toasts) != 1 || a.toasts[0].text != "Sent 2 books to KOReader (Kobo)" {
		t.Fatalf("toasts %+v", a.toasts)
	}
	if !a.connectedDevice().Has(a.byID[ids[0]].UUID) {
		t.Fatal("a book sent isn't known to be on KOReader")
	}

	k.conn.Close()
	for range 200 {
		pump()
		if a.connectedDevice() == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if a.connectedDevice() != nil {
		t.Fatal("KOReader going away wasn't noticed")
	}
}

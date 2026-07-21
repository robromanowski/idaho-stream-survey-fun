// Idaho Stream Finder — the genuine-Charm edition.
// huh forms + lip gloss rendering over the waters.json snapshot.
//
//	cd charm && go build -o streams-charm.exe . && ./streams-charm.exe
package main

import (
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	btable "github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

const waterURL = "https://idfg.idaho.gov/ifwis/fishingplanner/water/%s"

var (
	purple = lipgloss.Color("#7d56f4")
	pink   = lipgloss.Color("#ee6ff8")
	green  = lipgloss.Color("#04b575")
	grey   = lipgloss.Color("#6c6f85")
	cream  = lipgloss.Color("#fffdf5")

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(cream).
			Border(lipgloss.RoundedBorder()).BorderForeground(purple).Padding(0, 2)
	countStyle = lipgloss.NewStyle().Bold(true).Foreground(green)
	dimStyle   = lipgloss.NewStyle().Foreground(grey)
	byeStyle   = lipgloss.NewStyle().Foreground(pink)
	zebra      = lipgloss.AdaptiveColor{Light: "#f0eaff", Dark: "#221b3a"}

	chip = func(bg, fg string) lipgloss.Style {
		return lipgloss.NewStyle().Background(lipgloss.Color(bg)).
			Foreground(lipgloss.Color(fg)).Bold(true).Padding(0, 1)
	}
	// cast-for chips cycle cool water tones; steer-clear cycle warm warning tones
	incPalette = []lipgloss.Style{
		chip("#123a2c", "#2ee6a8"), // mint
		chip("#0f3440", "#53d6f0"), // cyan
		chip("#152b47", "#7aa8ff"), // sky
		chip("#1d3a1d", "#8ade7a"), // leaf
		chip("#0f3a36", "#5eead4"), // teal
		chip("#28214d", "#a89bff"), // periwinkle
	}
	excPalette = []lipgloss.Style{
		chip("#431129", "#ff7aa8"), // rose
		chip("#40190f", "#ff9c72"), // coral
		chip("#3a2c11", "#ffd166"), // amber
		chip("#3d1240", "#e583f2"), // magenta
	}
	chipWhere = lipgloss.NewStyle().Background(lipgloss.Color("#2a2440")).
			Foreground(lipgloss.Color("#c4b5fd")).Padding(0, 1)
	cardStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(purple).Padding(0, 2)
	cardLabel = lipgloss.NewStyle().Foreground(grey).Width(12)
)

// ---- gradient text (the charm signature) ----
func hexRGB(h string) (int, int, int) {
	h = strings.TrimPrefix(h, "#")
	r, _ := strconv.ParseInt(h[0:2], 16, 0)
	g, _ := strconv.ParseInt(h[2:4], 16, 0)
	b, _ := strconv.ParseInt(h[4:6], 16, 0)
	return int(r), int(g), int(b)
}

func gradient(s, from, to string) string {
	r1, g1, b1 := hexRGB(from)
	r2, g2, b2 := hexRGB(to)
	runes := []rune(s)
	n := len(runes)
	var out strings.Builder
	for i, r := range runes {
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		c := fmt.Sprintf("#%02x%02x%02x",
			int(math.Round(float64(r1)+t*float64(r2-r1))),
			int(math.Round(float64(g1)+t*float64(g2-g1))),
			int(math.Round(float64(b1)+t*float64(b2-b1))))
		out.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(c)).Render(string(r)))
	}
	return out.String()
}

type Water struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Var    *string `json:"var"`
	County string  `json:"county"`
	Trib   string  `json:"trib"`
	Layer  int     `json:"layer"`
	Size   string  `json:"size"`
	Region *int    `json:"region"`
	Sp     []int   `json:"sp"`
}

type Data struct {
	Generated string         `json:"generated"`
	Species   map[string]int `json:"species"`
	Regions   map[string]string
	Waters    []Water `json:"waters"`
}

var regionNames = map[int]string{1: "Panhandle", 2: "Clearwater", 3: "SW — Nampa", 8: "SW — McCall",
	4: "Magic Valley", 5: "Southeast", 6: "Upper Snake", 7: "Salmon"}

var groups = map[string][]string{
	"Any cutthroat": {"Cutthroat Trout", "Westslope Cutthroat Trout", "Yellowstone Cutthroat Trout",
		"Bonneville Cutthroat Trout", "Lahontan Cutthroat Trout", "Snake River Fine-spotted Cutthroat Trout"},
	"Brook trout (both)": {"Brook Trout", "Brook Trout - Triploid"},
	"Any cutbow/hybrid": {"Cutbow - Cutthroat x Rainbow Trout", "Rainbow x Cutthroat - Diploid",
		"Rainbow x Cutthroat - Triploid"},
}

var typicalTrout = []string{
	"Bonneville Cutthroat Trout", "Brook Trout", "Brook Trout - Triploid", "Brown Trout",
	"Bull Trout", "Cutbow - Cutthroat x Rainbow Trout", "Cutthroat Trout", "Golden Trout",
	"Lahontan Cutthroat Trout", "Lake Trout", "Lake Trout - Triploid", "Rainbow Trout",
	"Redband Trout", "Snake River Fine-spotted Cutthroat Trout", "Tiger Trout",
	"Westslope Cutthroat Trout", "Yellowstone Cutthroat Trout",
}

func loadData() (*Data, error) {
	exe, _ := os.Executable()
	for _, p := range []string{"waters.json", filepath.Join("..", "waters.json"),
		filepath.Join(filepath.Dir(exe), "waters.json"), filepath.Join(filepath.Dir(exe), "..", "waters.json")} {
		b, err := os.ReadFile(p)
		if err == nil {
			var d Data
			if err := json.Unmarshal(b, &d); err != nil {
				return nil, err
			}
			return &d, nil
		}
	}
	return nil, fmt.Errorf("waters.json not found — run: python build_data.py")
}

var famBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd166")).Italic(true)

func speciesOptions(d *Data) []huh.Option[string] {
	opts := []huh.Option[string]{}
	groupNames := make([]string, 0, len(groups))
	for g := range groups {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)
	for _, g := range groupNames {
		// name stays unstyled so huh can paint it green when selected;
		// the gold badge trails as a fixed marker
		label := g + famBadge.Render(fmt.Sprintf("   ✦ bundle of %d", len(groups[g])))
		opts = append(opts, huh.NewOption(label, "g:"+g))
	}
	for _, n := range typicalTrout {
		opts = append(opts, huh.NewOption(n, "s:"+n))
	}
	rest := make([]string, 0, len(d.Species))
	for n := range d.Species {
		if !contains(typicalTrout, n) {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	for _, n := range rest {
		opts = append(opts, huh.NewOption(dimStyle.Render(n), "s:"+n))
	}
	return opts
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func resolve(d *Data, picked []string) map[int]bool {
	ids := map[int]bool{}
	for _, p := range picked {
		if name, ok := strings.CutPrefix(p, "g:"); ok {
			for _, n := range groups[name] {
				ids[d.Species[n]] = true
			}
		} else if name, ok := strings.CutPrefix(p, "s:"); ok {
			ids[d.Species[name]] = true
		}
	}
	return ids
}

var leadingPunct = regexp.MustCompile(`^[^a-zA-Z0-9]+`)

func query(d *Data, inc, exc map[int]bool, region int, county, body string) []Water {
	var out []Water
	for _, w := range d.Waters {
		if body == "streams" && w.Layer != 1 {
			continue
		}
		if body == "lakes" && w.Layer == 1 {
			continue
		}
		if region != 0 && (w.Region == nil || *w.Region != region) {
			continue
		}
		if county != "" && !contains(strings.Split(w.County, ", "), county) {
			continue
		}
		if len(w.Sp) == 0 {
			continue // surveyed waters only, like the site default
		}
		has := func(set map[int]bool) bool {
			for _, s := range w.Sp {
				if set[s] {
					return true
				}
			}
			return false
		}
		if len(inc) > 0 && !has(inc) {
			continue
		}
		if len(exc) > 0 && has(exc) {
			continue
		}
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool {
		a := strings.ToLower(leadingPunct.ReplaceAllString(out[i].Name, ""))
		b := strings.ToLower(leadingPunct.ReplaceAllString(out[j].Name, ""))
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func banner(d *Data) string {
	title := gradient("I D A H O   S T R E A M   F I N D E R", "#7d56f4", "#ee6ff8")
	wave := gradient("≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈≈", "#04b575", "#7d56f4")
	sub := dimStyle.Render(fmt.Sprintf("%s waters · IDFG survey data · %s",
		humanize(len(d.Waters)), d.Generated))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(purple).
		Padding(0, 3).Render(title+"\n"+wave+"\n"+sub) + "\n"
}

func humanize(n int) string {
	s := strconv.Itoa(n)
	if n >= 1000 {
		s = s[:len(s)-3] + "," + s[len(s)-3:]
	}
	return s
}

// chips renders glyph-prefixed pills, cycling the palette, wrapped at ~72 columns.
func chips(names []string, palette []lipgloss.Style, glyph string) string {
	var lines []string
	var line []string
	width := 0
	for i, n := range names {
		label := glyph + n
		if width+len(label)+3 > 72 && len(line) > 0 {
			lines = append(lines, strings.Join(line, " "))
			line, width = nil, 0
		}
		line = append(line, palette[i%len(palette)].Render(label))
		width += len(label) + 3
	}
	if len(line) > 0 {
		lines = append(lines, strings.Join(line, " "))
	}
	return strings.Join(lines, "\n")
}

// summaryCard shows the chosen filters as aligned label + chip rows.
func summaryCard(d *Data, inc, exc map[int]bool, region int, county, body string) string {
	name := map[int]string{}
	for n, id := range d.Species {
		name[id] = n
	}
	pick := func(set map[int]bool) []string {
		var ns []string
		for id := range set {
			ns = append(ns, name[id])
		}
		sort.Strings(ns)
		return ns
	}
	row := func(label, content string) string {
		return lipgloss.JoinHorizontal(lipgloss.Top, cardLabel.Render(label), content)
	}
	var rows []string
	if len(inc) > 0 {
		rows = append(rows, row("cast for", chips(pick(inc), incPalette, "✓ ")))
	}
	if len(exc) > 0 {
		rows = append(rows, row("steer clear", chips(pick(exc), excPalette, "✕ ")))
	}
	whereParts := []string{}
	if region != 0 {
		whereParts = append(whereParts, regionNames[region]+" region")
	} else {
		whereParts = append(whereParts, "anywhere in Idaho")
	}
	if county != "" {
		whereParts = append(whereParts, county+" county")
	}
	whereParts = append(whereParts, body)
	rows = append(rows, row("where", chips(whereParts, []lipgloss.Style{chipWhere}, "")))
	return cardStyle.Render(strings.Join(rows, "\n\n")) + "\n"
}

// histogram draws gradient bars for the most-seen species across the results.
func histogram(d *Data, rows []Water) string {
	name := map[int]string{}
	for n, id := range d.Species {
		name[id] = n
	}
	freq := map[int]int{}
	for _, w := range rows {
		for _, s := range w.Sp {
			freq[s]++
		}
	}
	type kv struct {
		id, n int
	}
	var top []kv
	for id, n := range freq {
		top = append(top, kv{id, n})
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].n != top[j].n {
			return top[i].n > top[j].n
		}
		return name[top[i].id] < name[top[j].id]
	})
	if len(top) > 6 {
		top = top[:6]
	}
	if len(top) == 0 {
		return ""
	}
	maxN := top[0].n
	ramp := []string{"#7d56f4", "#9b63f2", "#b970f0", "#d77df4", "#ee6ff8", "#f68ad2"}
	var b strings.Builder
	b.WriteString(dimStyle.Render("who you'll meet out there") + "\n")
	for i, t := range top {
		w := int(math.Round(24 * float64(t.n) / float64(maxN)))
		if w < 1 {
			w = 1
		}
		bar := lipgloss.NewStyle().Foreground(lipgloss.Color(ramp[i%len(ramp)])).
			Render(strings.Repeat("█", w))
		label := fmt.Sprintf("%-36s", truncate(name[t.id], 35)) // pad BEFORE styling: ANSI breaks %-42s
		b.WriteString(fmt.Sprintf("%s %s %s\n",
			lipgloss.NewStyle().Foreground(cream).Render(label),
			bar, countStyle.Render(strconv.Itoa(t.n))))
	}
	return b.String()
}

func renderTable(d *Data, rows []Water, limit int) string {
	name := map[int]string{}
	for n, id := range d.Species {
		name[id] = n
	}
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(purple)).
		Headers("WATER", "COUNTY", "DRAINAGE", "SIZE", "SPECIES OBSERVED").
		StyleFunc(func(row, col int) lipgloss.Style {
			s := lipgloss.NewStyle().Padding(0, 1)
			if row != table.HeaderRow && row%2 == 1 {
				s = s.Background(zebra)
			}
			switch {
			case row == table.HeaderRow:
				return s.Bold(true).Foreground(pink)
			case col == 0:
				return s.Bold(true).Foreground(cream)
			case col == 3:
				return s.Foreground(green).Align(lipgloss.Right)
			case col == 4:
				return s.Foreground(lipgloss.Color("#7cc7ad"))
			default:
				return s.Foreground(grey)
			}
		})
	for i, w := range rows {
		if i >= limit {
			break
		}
		n := w.Name
		if w.Var != nil && *w.Var != "" {
			n += " (" + *w.Var + ")"
		}
		unit := " mi"
		if w.Layer != 1 {
			unit = " ac"
		}
		size := ""
		if w.Size != "" {
			size = w.Size + unit
		}
		sp := make([]string, 0, len(w.Sp))
		for _, s := range w.Sp {
			sp = append(sp, name[s])
		}
		sort.Strings(sp)
		t.Row(truncate(n, 30), truncate(w.County, 14), truncate(w.Trib, 32), size,
			truncate(strings.Join(sp, ", "), 40))
	}
	return t.Render()
}

// ---- stream geometry (same ArcGIS service the IDFG map uses) ----
const hydroURL = "https://gisportal-idfg.idaho.gov/hosting/rest/services/Hydrography/Hydrography_Public/MapServer"

type polyline [][2]float64 // lon, lat

func fetchGeom(w Water) ([]polyline, error) {
	layer := 1
	if w.Layer != 1 {
		layer = 0
	}
	q := url.Values{
		"where": {fmt.Sprintf("LLID = '%s'", w.ID)}, "outFields": {"LLID"},
		"f": {"geojson"}, "outSR": {"4326"},
	}
	client := http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/%d/query?%s", hydroURL, layer, q.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var gj struct {
		Features []struct {
			Geometry struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(body, &gj); err != nil {
		return nil, err
	}
	var lines []polyline
	addLine := func(raw [][]float64) {
		var pl polyline
		for _, p := range raw {
			if len(p) >= 2 {
				pl = append(pl, [2]float64{p[0], p[1]})
			}
		}
		if len(pl) > 1 {
			lines = append(lines, pl)
		}
	}
	for _, f := range gj.Features {
		switch f.Geometry.Type {
		case "LineString":
			var c [][]float64
			if json.Unmarshal(f.Geometry.Coordinates, &c) == nil {
				addLine(c)
			}
		case "MultiLineString", "Polygon":
			var c [][][]float64
			if json.Unmarshal(f.Geometry.Coordinates, &c) == nil {
				for _, l := range c {
					addLine(l)
				}
			}
		case "MultiPolygon":
			var c [][][][]float64
			if json.Unmarshal(f.Geometry.Coordinates, &c) == nil {
				for _, poly := range c {
					for _, l := range poly {
						addLine(l)
					}
				}
			}
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("no geometry returned")
	}
	return lines, nil
}

// ---- braille canvas: 2x4 sub-dots per terminal cell ----
type canvas struct {
	w, h  int // in cells
	cells []rune
}

var brailleBits = [4][2]rune{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

func newCanvas(w, h int) *canvas {
	return &canvas{w: w, h: h, cells: make([]rune, w*h)}
}

func (c *canvas) set(x, y int) {
	if x < 0 || y < 0 || x >= c.w*2 || y >= c.h*4 {
		return
	}
	c.cells[(y/4)*c.w+x/2] |= brailleBits[y%4][x%2]
}

func (c *canvas) line(x0, y0, x1, y1 int) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	e := dx + dy
	for {
		c.set(x0, y0)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * e // must be computed once, before either branch mutates e
		if e2 >= dy {
			e += dy
			x0 += sx
		}
		if e2 <= dx {
			e += dx
			y0 += sy
		}
	}
}

func (c *canvas) render() string {
	var b strings.Builder
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			r := c.cells[y*c.w+x]
			if r == 0 {
				b.WriteRune(' ')
			} else {
				b.WriteRune(0x2800 + r)
			}
		}
		if y < c.h-1 {
			b.WriteRune('\n')
		}
	}
	return b.String()
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// ---- statewide overview: counties + every stream in the result set ----

//go:embed idaho_counties.geojson
var countiesRaw []byte

var (
	countiesOnce  sync.Once
	countiesLines []polyline
)

func countyLines() []polyline {
	countiesOnce.Do(func() {
		var gj struct {
			Features []struct {
				Geometry struct {
					Type        string          `json:"type"`
					Coordinates json.RawMessage `json:"coordinates"`
				} `json:"geometry"`
			} `json:"features"`
		}
		if json.Unmarshal(countiesRaw, &gj) != nil {
			return
		}
		add := func(raw [][]float64) {
			var pl polyline
			for _, p := range raw {
				if len(p) >= 2 {
					pl = append(pl, [2]float64{p[0], p[1]})
				}
			}
			if len(pl) > 1 {
				countiesLines = append(countiesLines, pl)
			}
		}
		for _, f := range gj.Features {
			switch f.Geometry.Type {
			case "Polygon":
				var c [][][]float64
				if json.Unmarshal(f.Geometry.Coordinates, &c) == nil {
					for _, ring := range c {
						add(ring)
					}
				}
			case "MultiPolygon":
				var c [][][][]float64
				if json.Unmarshal(f.Geometry.Coordinates, &c) == nil {
					for _, poly := range c {
						for _, ring := range poly {
							add(ring)
						}
					}
				}
			}
		}
	})
	return countiesLines
}

// ---- reference map: regions + counties with labels ----

//go:embed idaho_regions.geojson
var regionsRaw []byte

type refFeature struct {
	label    string
	sublabel string
	lines    []polyline
	centroid [2]float64
	area     float64 // rough bbox area, for label priority
}

func parseRefFeatures(raw []byte, labelOf func(props map[string]any) (string, string)) []refFeature {
	var gj struct {
		Features []struct {
			Properties map[string]any `json:"properties"`
			Geometry   struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if json.Unmarshal(raw, &gj) != nil {
		return nil
	}
	var out []refFeature
	for _, f := range gj.Features {
		rf := refFeature{}
		rf.label, rf.sublabel = labelOf(f.Properties)
		addRing := func(raw [][]float64) {
			var pl polyline
			for _, p := range raw {
				if len(p) >= 2 {
					pl = append(pl, [2]float64{p[0], p[1]})
				}
			}
			if len(pl) > 1 {
				rf.lines = append(rf.lines, pl)
			}
		}
		switch f.Geometry.Type {
		case "Polygon":
			var c [][][]float64
			if json.Unmarshal(f.Geometry.Coordinates, &c) == nil {
				for _, ring := range c {
					addRing(ring)
				}
			}
		case "MultiPolygon":
			var c [][][][]float64
			if json.Unmarshal(f.Geometry.Coordinates, &c) == nil {
				for _, poly := range c {
					for _, ring := range poly {
						addRing(ring)
					}
				}
			}
		}
		// centroid of the biggest ring is a decent label anchor
		best := 0
		for i, l := range rf.lines {
			if len(l) > len(rf.lines[best]) {
				best = i
			}
		}
		if len(rf.lines) > 0 {
			var sx, sy float64
			minLo, minLa := math.Inf(1), math.Inf(1)
			maxLo, maxLa := math.Inf(-1), math.Inf(-1)
			for _, p := range rf.lines[best] {
				sx += p[0]
				sy += p[1]
				minLo, maxLo = math.Min(minLo, p[0]), math.Max(maxLo, p[0])
				minLa, maxLa = math.Min(minLa, p[1]), math.Max(maxLa, p[1])
			}
			n := float64(len(rf.lines[best]))
			rf.centroid = [2]float64{sx / n, sy / n}
			rf.area = (maxLo - minLo) * (maxLa - minLa)
		}
		out = append(out, rf)
	}
	return out
}

var (
	refOnce     sync.Once
	refRegions  []refFeature
	refCounties []refFeature
)

func refData() ([]refFeature, []refFeature) {
	refOnce.Do(func() {
		refRegions = parseRefFeatures(regionsRaw, func(p map[string]any) (string, string) {
			id := 0
			if v, ok := p["ID"].(float64); ok {
				id = int(v)
			}
			short, _ := p["NmShort"].(string)
			return strconv.Itoa(id), short
		})
		refCounties = parseRefFeatures(countiesRaw, func(p map[string]any) (string, string) {
			n, _ := p["NAME"].(string)
			return n, ""
		})
	})
	return refRegions, refCounties
}

// renderRefMap draws regions + counties; mode 0 highlights regions, 1 counties.
func renderRefMap(w, h, mode int) string {
	regions, counties := refData()
	nx0, nx1 := mercX(-117.35), mercX(-110.95)
	ny0, ny1 := mercY(49.1), mercY(41.9)
	dotW, dotH := float64(w*2-1), float64(h*4-1)
	k := math.Min(dotW/(nx1-nx0), dotH/(ny1-ny0))
	cx := nx0 - (dotW/k-(nx1-nx0))/2
	cy := ny0 - (dotH/k-(ny1-ny0))/2
	toDot := func(p [2]float64) (int, int) {
		return int(math.Round((mercX(p[0]) - cx) * k)), int(math.Round((mercY(p[1]) - cy) * k))
	}
	drawFeat := func(c *canvas, feats []refFeature) {
		for _, f := range feats {
			for _, l := range f.lines {
				for i := 1; i < len(l); i++ {
					x0, y0 := toDot(l[i-1])
					x1, y1 := toDot(l[i])
					c.line(x0, y0, x1, y1)
				}
			}
		}
	}
	regC := newCanvas(w, h)
	drawFeat(regC, regions)
	cntC := newCanvas(w, h)
	drawFeat(cntC, counties)

	// label layer: chars stamped over the linework
	type stamp struct {
		ch    rune
		class int // 1 = primary label, 2 = secondary label
	}
	labels := make([]stamp, w*h)
	free := func(y, x0, n int) bool { // row y, cells [x0, x0+n) all unlabeled + in bounds
		if y < 0 || y >= h || x0 < 0 || x0+n > w {
			return false
		}
		for x := x0; x < x0+n; x++ {
			if labels[y*w+x].ch != 0 {
				return false
			}
		}
		return true
	}
	write := func(y, x0 int, text string, class int) {
		for i, r := range []rune(text) {
			labels[y*w+x0+i] = stamp{r, class}
		}
	}
	// place tries the anchor row, one row down, one row up, then a truncated
	// form, and gives up rather than colliding with an earlier (larger) label
	place := func(f refFeature, text string, class int, dy int) {
		if text == "" {
			return
		}
		dx, dyD := toDot(f.centroid)
		cxCell, cyCell := dx/2, dyD/4+dy
		for _, cand := range []string{text, truncate(text, 5)} {
			n := len([]rune(cand))
			x0 := cxCell - n/2
			for _, yy := range []int{cyCell, cyCell + 1, cyCell - 1} {
				if free(yy, x0, n) {
					write(yy, x0, cand, class)
					return
				}
			}
		}
	}
	if mode == 0 {
		for _, f := range regions {
			place(f, f.label, 1, 0)
			place(f, f.sublabel, 2, 1)
		}
	} else {
		byArea := append([]refFeature(nil), counties...)
		sort.Slice(byArea, func(i, j int) bool { return byArea[i].area > byArea[j].area })
		for _, f := range byArea {
			place(f, f.label, 1, 0)
		}
	}

	var b strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			l := labels[y*w+x]
			rBits := regC.cells[y*regC.w+x]
			nBits := cntC.cells[y*cntC.w+x]
			switch {
			case l.ch != 0 && l.class == 1:
				if mode == 0 {
					b.WriteString("\x1b[38;2;238;111;248m\x1b[1m") // region number, pink bold
				} else {
					b.WriteString("\x1b[38;2;196;181;253m") // county name, lavender
				}
				b.WriteRune(l.ch)
				b.WriteString("\x1b[0m")
			case l.ch != 0:
				b.WriteString("\x1b[38;2;154;140;200m") // region short name
				b.WriteRune(l.ch)
				b.WriteString("\x1b[0m")
			case mode == 0 && rBits != 0:
				b.WriteString("\x1b[38;2;255;253;245m\x1b[1m") // region borders, bright
				b.WriteRune(0x2800 + rBits)
				b.WriteString("\x1b[0m")
			case mode == 1 && nBits != 0:
				b.WriteString("\x1b[38;2;196;181;253m") // county borders, lavender
				b.WriteRune(0x2800 + nBits)
				b.WriteString("\x1b[0m")
			default:
				b.WriteRune(' ')
			}
		}
		if y < h-1 {
			b.WriteRune('\n')
		}
	}
	return b.String()
}

// refMapModel is a tiny standalone viewer for the reference map.
type refMapModel struct {
	mode   int
	width  int
	height int
}

func (m refMapModel) Init() tea.Cmd { return nil }

func (m refMapModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "enter", "ctrl+c":
			return m, tea.Quit
		case "tab", "t", "c", "r", " ":
			m.mode = 1 - m.mode
			return m, nil
		}
	}
	return m, nil
}

func (m refMapModel) View() string {
	mapW := maxi(40, m.width-4)
	mapH := maxi(12, m.height-5)
	title := "IDFG regions"
	if m.mode == 1 {
		title = "Idaho counties"
	}
	head := lipgloss.NewStyle().Bold(true).Foreground(cream).Background(purple).Padding(0, 2).
		Render(title) + dimStyle.Render("  reference map")
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(purple).
		Padding(0, 1).Width(mapW).Render(renderRefMap(mapW-4, mapH-2, m.mode))
	help := dimStyle.Render("  ") + lipgloss.NewStyle().Foreground(pink).Render("tab") +
		dimStyle.Render(" regions ⇄ counties · ") +
		lipgloss.NewStyle().Foreground(pink).Render("enter/esc") + dimStyle.Render(" back to the form")
	return head + "\n" + box + "\n" + help
}

func runRefMap() {
	m := refMapModel{width: 120, height: 34}
	_, _ = tea.NewProgram(m, tea.WithAltScreen()).Run()
}

// whereModel wraps the region/county form so "?" (or F1) can pop the reference
// map mid-question — a real hotkey instead of a fake menu option.
type whereModel struct {
	form    *huh.Form
	width   int
	height  int
	showMap bool
	mapMode int
}

func (m whereModel) Init() tea.Cmd { return m.form.Init() }

func (m whereModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if m.showMap {
			switch msg.String() {
			case "q", "esc", "enter", "?", "f1", "ctrl+c":
				m.showMap = false
			case "tab", "t", "c", "r", " ":
				m.mapMode = 1 - m.mapMode
			}
			return m, nil
		}
		if s := msg.String(); s == "?" || s == "f1" {
			m.showMap = true
			return m, nil
		}
	}
	f, cmd := m.form.Update(msg)
	if ff, ok := f.(*huh.Form); ok {
		m.form = ff
	}
	if m.form.State == huh.StateCompleted || m.form.State == huh.StateAborted {
		return m, tea.Quit
	}
	return m, cmd
}

func (m whereModel) View() string {
	if m.showMap {
		ref := refMapModel{mode: m.mapMode, width: maxi(m.width, 80), height: maxi(m.height-1, 28)}
		return ref.View()
	}
	hint := dimStyle.Render("  ") + lipgloss.NewStyle().Foreground(pink).Render("?") +
		dimStyle.Render(" map of regions & counties")
	return m.form.View() + "\n" + hint
}

// fetchResultGeoms pulls simplified geometry for every water in the result set,
// in chunks, keyed by LLID. maxAllowableOffset keeps responses small at state scale.
func fetchResultGeoms(rows []Water) (map[string][]polyline, error) {
	out := map[string][]polyline{}
	client := http.Client{Timeout: 40 * time.Second}
	for layer := 0; layer <= 1; layer++ {
		var ids []string
		for _, w := range rows {
			if (w.Layer == 1) == (layer == 1) {
				ids = append(ids, w.ID)
			}
		}
		for i := 0; i < len(ids); i += 50 {
			chunk := ids[i:min(i+50, len(ids))]
			quoted := make([]string, len(chunk))
			for j, id := range chunk {
				quoted[j] = "'" + id + "'"
			}
			q := url.Values{
				"where":              {"LLID IN (" + strings.Join(quoted, ",") + ")"},
				"outFields":          {"LLID"},
				"f":                  {"geojson"},
				"outSR":              {"4326"},
				"maxAllowableOffset": {"0.004"},
			}
			resp, err := client.Get(fmt.Sprintf("%s/%d/query?%s", hydroURL, layer, q.Encode()))
			if err != nil {
				return out, err
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return out, err
			}
			var gj struct {
				Features []struct {
					Properties struct {
						LLID string `json:"LLID"`
					} `json:"properties"`
					Geometry struct {
						Type        string          `json:"type"`
						Coordinates json.RawMessage `json:"coordinates"`
					} `json:"geometry"`
				} `json:"features"`
			}
			if err := json.Unmarshal(body, &gj); err != nil {
				return out, err
			}
			for _, f := range gj.Features {
				add := func(raw [][]float64) {
					var pl polyline
					for _, p := range raw {
						if len(p) >= 2 {
							pl = append(pl, [2]float64{p[0], p[1]})
						}
					}
					if len(pl) > 1 {
						out[f.Properties.LLID] = append(out[f.Properties.LLID], pl)
					}
				}
				switch f.Geometry.Type {
				case "LineString":
					var c [][]float64
					if json.Unmarshal(f.Geometry.Coordinates, &c) == nil {
						add(c)
					}
				case "MultiLineString", "Polygon":
					var c [][][]float64
					if json.Unmarshal(f.Geometry.Coordinates, &c) == nil {
						for _, l := range c {
							add(l)
						}
					}
				case "MultiPolygon":
					var c [][][][]float64
					if json.Unmarshal(f.Geometry.Coordinates, &c) == nil {
						for _, poly := range c {
							for _, l := range poly {
								add(l)
							}
						}
					}
				}
			}
		}
	}
	return out, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---- context map: neighbor streams + topo tile backdrop ----
type mapData struct {
	main, nbrs         []polyline
	img                *image.RGBA // stitched topo tiles (nil if unavailable)
	imgPx0             int         // world-pixel x of img top-left at zoom
	imgPy0             int
	zoom               int
	nx0, nx1, ny0, ny1 float64 // viewport in normalized mercator (aspect-matched)
}

// mercator normalized coords in [0,1]
func mercX(lon float64) float64 { return (lon + 180) / 360 }
func mercY(lat float64) float64 {
	la := lat * math.Pi / 180
	return (1 - math.Log(math.Tan(la)+1/math.Cos(la))/math.Pi) / 2
}
func invMercLon(nx float64) float64 { return nx*360 - 180 }
func invMercLat(ny float64) float64 {
	return math.Atan(math.Sinh(math.Pi*(1-2*ny))) * 180 / math.Pi
}

// fetchNeighbors returns streams intersecting the bbox (excluding the water itself).
func fetchNeighbors(exceptID string, minLon, minLat, maxLon, maxLat float64) ([]polyline, error) {
	q := url.Values{
		"geometry":          {fmt.Sprintf("%f,%f,%f,%f", minLon, minLat, maxLon, maxLat)},
		"geometryType":      {"esriGeometryEnvelope"},
		"inSR":              {"4326"},
		"spatialRel":        {"esriSpatialRelIntersects"},
		"outFields":         {"LLID"},
		"resultRecordCount": {"300"},
		"f":                 {"geojson"},
		"outSR":             {"4326"},
	}
	client := http.Client{Timeout: 25 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/1/query?%s", hydroURL, q.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var gj struct {
		Features []struct {
			Properties struct {
				LLID string `json:"LLID"`
			} `json:"properties"`
			Geometry struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(body, &gj); err != nil {
		return nil, err
	}
	var lines []polyline
	addLine := func(raw [][]float64) {
		var pl polyline
		for _, p := range raw {
			if len(p) >= 2 {
				pl = append(pl, [2]float64{p[0], p[1]})
			}
		}
		if len(pl) > 1 {
			lines = append(lines, pl)
		}
	}
	for _, f := range gj.Features {
		if f.Properties.LLID == exceptID {
			continue
		}
		switch f.Geometry.Type {
		case "LineString":
			var c [][]float64
			if json.Unmarshal(f.Geometry.Coordinates, &c) == nil {
				addLine(c)
			}
		case "MultiLineString":
			var c [][][]float64
			if json.Unmarshal(f.Geometry.Coordinates, &c) == nil {
				for _, l := range c {
					addLine(l)
				}
			}
		}
	}
	return lines, nil
}

// fetchTopo stitches Esri World Topo tiles covering the bbox at the given zoom.
func fetchTopo(minLon, minLat, maxLon, maxLat float64, zoom int) (*image.RGBA, int, int, error) {
	n := 1 << zoom
	txMin := int(mercX(minLon) * float64(n))
	txMax := int(mercX(maxLon) * float64(n))
	tyMin := int(mercY(maxLat) * float64(n)) // lat max = top = smaller y
	tyMax := int(mercY(minLat) * float64(n))
	if (txMax-txMin+1)*(tyMax-tyMin+1) > 20 {
		return nil, 0, 0, fmt.Errorf("bbox needs too many tiles at z%d", zoom)
	}
	img := image.NewRGBA(image.Rect(0, 0, (txMax-txMin+1)*256, (tyMax-tyMin+1)*256))
	client := http.Client{Timeout: 25 * time.Second}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for ty := tyMin; ty <= tyMax; ty++ {
		for tx := txMin; tx <= txMax; tx++ {
			wg.Add(1)
			go func(tx, ty int) {
				defer wg.Done()
				u := fmt.Sprintf("https://server.arcgisonline.com/ArcGIS/rest/services/World_Topo_Map/MapServer/tile/%d/%d/%d", zoom, ty, tx)
				resp, err := client.Get(u)
				if err == nil {
					defer resp.Body.Close()
					if tile, _, derr := image.Decode(resp.Body); derr == nil {
						mu.Lock()
						draw.Draw(img, image.Rect((tx-txMin)*256, (ty-tyMin)*256,
							(tx-txMin+1)*256, (ty-tyMin+1)*256), tile, image.Point{}, draw.Src)
						mu.Unlock()
						return
					} else {
						err = derr
					}
				}
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}(tx, ty)
		}
	}
	wg.Wait()
	if firstErr != nil {
		return nil, 0, 0, firstErr
	}
	return img, txMin * 256, tyMin * 256, nil
}

// buildMapData gathers everything the water-page map needs (called off the UI
// thread). aspect is the target viewport's dot-grid width/height ratio, so the
// fetched area fills the whole panel rather than just the stream's own bbox.
func buildMapData(w Water, aspect float64) (*mapData, error) {
	main, err := fetchGeom(w)
	if err != nil {
		return nil, err
	}
	md := &mapData{main: main}
	minLon, minLat := math.Inf(1), math.Inf(1)
	maxLon, maxLat := math.Inf(-1), math.Inf(-1)
	for _, l := range main {
		for _, p := range l {
			minLon, maxLon = math.Min(minLon, p[0]), math.Max(maxLon, p[0])
			minLat, maxLat = math.Min(minLat, p[1]), math.Max(maxLat, p[1])
		}
	}
	// pad, then expand the mercator bbox to the viewport aspect so tiles and
	// neighbors cover everything the canvas will show
	nx0, nx1 := mercX(minLon), mercX(maxLon)
	ny0, ny1 := mercY(maxLat), mercY(minLat)
	padX := math.Max((nx1-nx0)*0.12, 2e-6)
	padY := math.Max((ny1-ny0)*0.12, 2e-6)
	nx0, nx1, ny0, ny1 = nx0-padX, nx1+padX, ny0-padY, ny1+padY
	if aspect <= 0 {
		aspect = 3
	}
	spanX, spanY := nx1-nx0, ny1-ny0
	if spanX/spanY < aspect { // too narrow: widen X
		grow := (spanY*aspect - spanX) / 2
		nx0, nx1 = nx0-grow, nx1+grow
	} else { // too wide: grow Y
		grow := (spanX/aspect - spanY) / 2
		ny0, ny1 = ny0-grow, ny1+grow
	}
	md.nx0, md.nx1, md.ny0, md.ny1 = nx0, nx1, ny0, ny1
	minLon, maxLon = invMercLon(nx0), invMercLon(nx1)
	maxLat, minLat = invMercLat(ny0), invMercLat(ny1)

	if nbrs, err := fetchNeighbors(w.ID, minLon, minLat, maxLon, maxLat); err == nil {
		md.nbrs = nbrs
	}
	// pick a zoom that gives roughly 1 tile pixel per braille dot for a ~190-dot map
	zoom := int(math.Ceil(math.Log2(190 / (256 * (nx1 - nx0)))))
	if zoom < 7 {
		zoom = 7
	}
	if zoom > 15 {
		zoom = 15
	}
	for z := zoom; z >= 7; z-- {
		if img, px0, py0, err := fetchTopo(minLon, minLat, maxLon, maxLat, z); err == nil {
			md.img, md.imgPx0, md.imgPy0, md.zoom = img, px0, py0, z
			break
		}
	}
	return md, nil
}

// renderMap composes backdrop + neighbor streams + main trace into ANSI art.
// Braille dots are ~square when terminal cells are 1:2, so a uniform mercator
// scale keeps shapes true.
func renderMap(md *mapData, w, h int, backdrop bool) string {
	nx0, nx1, ny0, ny1 := md.nx0, md.nx1, md.ny0, md.ny1
	dotW, dotH := float64(w*2-1), float64(h*4-1)
	k := math.Min(dotW/(nx1-nx0), dotH/(ny1-ny0))
	cx := nx0 - (dotW/k-(nx1-nx0))/2 // recenter so any leftover splits evenly
	cy := ny0 - (dotH/k-(ny1-ny0))/2

	toDot := func(p [2]float64) (int, int) {
		return int(math.Round((mercX(p[0]) - cx) * k)), int(math.Round((mercY(p[1]) - cy) * k))
	}
	drawLines := func(c *canvas, lines []polyline) {
		for _, l := range lines {
			for i := 1; i < len(l); i++ {
				x0, y0 := toDot(l[i-1])
				x1, y1 := toDot(l[i])
				c.line(x0, y0, x1, y1)
			}
		}
	}
	mainC := newCanvas(w, h)
	drawLines(mainC, md.main)
	nbrC := newCanvas(w, h)
	drawLines(nbrC, md.nbrs)

	// per-cell backdrop color sampled from the stitched topo image
	bgAt := func(cellX, cellY int) (r, g, b int, ok bool) {
		if !backdrop || md.img == nil {
			return 0, 0, 0, false
		}
		worldPx := float64(int(256) << md.zoom)
		nX := cx + (float64(cellX*2) + 1) / k
		nY := cy + (float64(cellY*4) + 2) / k
		ix := int(nX*worldPx) - md.imgPx0
		iy := int(nY*worldPx) - md.imgPy0
		if ix < 0 || iy < 0 || ix >= md.img.Rect.Dx() || iy >= md.img.Rect.Dy() {
			return 0, 0, 0, false
		}
		cr, cg, cb, _ := md.img.At(ix, iy).RGBA()
		// dim the basemap so the traces pop
		return int(float64(cr>>8) * 0.60), int(float64(cg>>8) * 0.60), int(float64(cb>>8) * 0.60), true
	}

	var b strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			mBits := mainC.cells[y*mainC.w+x]
			nBits := nbrC.cells[y*nbrC.w+x]
			r, g, bl, hasBg := bgAt(x, y)
			if hasBg {
				b.WriteString(fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, bl))
			}
			switch {
			case mBits != 0:
				b.WriteString("\x1b[38;2;83;214;240m\x1b[1m") // cyan, bold
				b.WriteRune(0x2800 + (mBits | nBits))
				b.WriteString("\x1b[22m")
			case nBits != 0:
				b.WriteString("\x1b[38;2;122;144;180m") // steel blue, dim
				b.WriteRune(0x2800 + nBits)
			default:
				b.WriteRune(' ')
			}
			b.WriteString("\x1b[0m")
		}
		if y < h-1 {
			b.WriteRune('\n')
		}
	}
	return b.String()
}

// traceMap draws the polylines into a w×h cell braille map, north up.
func traceMap(lines []polyline, w, h int) string {
	minLon, minLat := math.Inf(1), math.Inf(1)
	maxLon, maxLat := math.Inf(-1), math.Inf(-1)
	for _, l := range lines {
		for _, p := range l {
			minLon, maxLon = math.Min(minLon, p[0]), math.Max(maxLon, p[0])
			minLat, maxLat = math.Min(minLat, p[1]), math.Max(maxLat, p[1])
		}
	}
	kx := math.Cos((minLat + maxLat) / 2 * math.Pi / 180)
	worldW := (maxLon - minLon) * kx
	worldH := maxLat - minLat
	if worldW <= 0 {
		worldW = 1e-9
	}
	if worldH <= 0 {
		worldH = 1e-9
	}
	dotW, dotH := float64(w*2-1), float64(h*4-1)
	scale := math.Min(dotW/worldW, dotH/worldH) * 0.94
	ox := (dotW - worldW*scale) / 2
	oy := (dotH - worldH*scale) / 2
	c := newCanvas(w, h)
	px := func(p [2]float64) (int, int) {
		x := ox + (p[0]-minLon)*kx*scale
		y := dotH - (oy + (p[1]-minLat)*scale)
		return int(math.Round(x)), int(math.Round(y))
	}
	for _, l := range lines {
		for i := 1; i < len(l); i++ {
			x0, y0 := px(l[i-1])
			x1, y1 := px(l[i])
			c.line(x0, y0, x1, y1)
		}
	}
	return c.render()
}

// ---- browse mode: full results table with arrow keys ----
type browseModel struct {
	rows   []Water
	spName map[int]string
	tbl    btable.Model
	width  int
	height int
	note   string

	detail    bool // showing the in-TUI water page
	backdrop  bool // topo tile background on the trace map
	geomCache map[string]*mapData
	geomErr   map[string]string
	loading   string // LLID currently being fetched

	// statewide overview
	overview  bool
	ovGeoms   map[string][]polyline // LLID -> simplified lines
	ovLoading bool
	ovErr     string
	ovImg     *image.RGBA
	ovPx0     int
	ovPy0     int
	ovZoom    int
	ovBox     [4]float64 // nx0, nx1, ny0, ny1
}

type ovMsg struct {
	geoms map[string][]polyline
	img   *image.RGBA
	px0   int
	py0   int
	zoom  int
	box   [4]float64
	err   error
}

// buildOverviewCmd fetches simplified geometry for the whole result set plus
// statewide topo tiles, viewport-matched to the given aspect.
func buildOverviewCmd(rows []Water, aspect float64) tea.Cmd {
	return func() tea.Msg {
		// Idaho bounds, padded
		nx0, nx1 := mercX(-117.35), mercX(-110.95)
		ny0, ny1 := mercY(49.1), mercY(41.9)
		spanX, spanY := nx1-nx0, ny1-ny0
		if aspect <= 0 {
			aspect = 2
		}
		if spanX/spanY < aspect {
			grow := (spanY*aspect - spanX) / 2
			nx0, nx1 = nx0-grow, nx1+grow
		} else {
			grow := (spanX/aspect - spanY) / 2
			ny0, ny1 = ny0-grow, ny1+grow
		}
		msg := ovMsg{box: [4]float64{nx0, nx1, ny0, ny1}}
		msg.geoms, msg.err = fetchResultGeoms(rows)
		zoom := int(math.Ceil(math.Log2(300 / (256 * (nx1 - nx0)))))
		if zoom < 5 {
			zoom = 5
		}
		if zoom > 9 {
			zoom = 9
		}
		for z := zoom; z >= 5; z-- {
			if img, px0, py0, err := fetchTopo(invMercLon(nx0), invMercLat(ny1), invMercLon(nx1), invMercLat(ny0), z); err == nil {
				msg.img, msg.px0, msg.py0, msg.zoom = img, px0, py0, z
				break
			}
		}
		return msg
	}
}

type geomMsg struct {
	id   string
	data *mapData
	err  error
}

func fetchGeomCmd(w Water, aspect float64) tea.Cmd {
	return func() tea.Msg {
		data, err := buildMapData(w, aspect)
		return geomMsg{id: w.ID, data: data, err: err}
	}
}

func newBrowse(d *Data, rows []Water) browseModel {
	spName := map[int]string{}
	for n, id := range d.Species {
		spName[id] = n
	}
	t := btable.New(btable.WithFocused(true))
	st := btable.DefaultStyles()
	st.Header = st.Header.Bold(true).Foreground(pink).BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(purple).BorderBottom(true)
	st.Selected = st.Selected.Foreground(cream).Background(purple).Bold(true)
	t.SetStyles(st)
	m := browseModel{rows: rows, spName: spName, tbl: t, width: 120, height: 34, backdrop: true,
		geomCache: map[string]*mapData{}, geomErr: map[string]string{}}
	m.applySize()
	return m
}

// current returns the selected water, if any.
func (m *browseModel) current() (Water, bool) {
	if c := m.tbl.Cursor(); c >= 0 && c < len(m.rows) {
		return m.rows[c], true
	}
	return Water{}, false
}

// mapDims returns the map panel's outer cell size for the current window.
func (m *browseModel) mapDims() (int, int) {
	mapW := maxi(30, m.width-42-10)
	mapH := maxi(10, m.height-8)
	return mapW, mapH
}

// mapAspect is the dot-grid width/height ratio of the map canvas.
func (m *browseModel) mapAspect() float64 {
	mapW, mapH := m.mapDims()
	return float64((mapW-4)*2 - 1) / float64((mapH-2)*4-1)
}

// loadCurrent kicks off a geometry fetch for the selected water if needed.
func (m *browseModel) loadCurrent() tea.Cmd {
	w, ok := m.current()
	if !ok {
		return nil
	}
	if _, have := m.geomCache[w.ID]; have {
		return nil
	}
	if _, failed := m.geomErr[w.ID]; failed {
		return nil
	}
	m.loading = w.ID
	return fetchGeomCmd(w, m.mapAspect())
}

func (m *browseModel) applySize() {
	inner := m.width - 6
	fixed := 26 + 12 + 28 + 8
	spW := maxi(15, inner-10-fixed)
	m.tbl.SetColumns([]btable.Column{
		{Title: "Water", Width: 26}, {Title: "County", Width: 12}, {Title: "Drainage", Width: 28},
		{Title: "Size", Width: 8}, {Title: "Species", Width: spW},
	})
	m.tbl.SetWidth(inner)
	m.tbl.SetHeight(maxi(5, m.height-10))
	trs := make([]btable.Row, 0, len(m.rows))
	for _, w := range m.rows {
		n := w.Name
		if w.Var != nil && *w.Var != "" {
			n += " (" + *w.Var + ")"
		}
		unit := " mi"
		if w.Layer != 1 {
			unit = " ac"
		}
		size := ""
		if w.Size != "" {
			size = w.Size + unit
		}
		sp := make([]string, 0, len(w.Sp))
		for _, s := range w.Sp {
			sp = append(sp, m.spName[s])
		}
		sort.Strings(sp)
		trs = append(trs, btable.Row{n, w.County, w.Trib, size, strings.Join(sp, ", ")})
	}
	m.tbl.SetRows(trs)
}

func (m browseModel) Init() tea.Cmd { return nil }

func (m browseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.applySize()
		return m, nil

	case geomMsg:
		if msg.err != nil {
			m.geomErr[msg.id] = msg.err.Error()
		} else {
			m.geomCache[msg.id] = msg.data
		}
		if m.loading == msg.id {
			m.loading = ""
		}
		return m, nil

	case ovMsg:
		m.ovLoading = false
		if msg.err != nil && len(msg.geoms) == 0 {
			m.ovErr = msg.err.Error()
		}
		m.ovGeoms = msg.geoms
		m.ovImg, m.ovPx0, m.ovPy0, m.ovZoom = msg.img, msg.px0, msg.py0, msg.zoom
		m.ovBox = msg.box
		return m, nil

	case tea.KeyMsg:
		if m.overview {
			switch msg.String() {
			case "q", "esc", "i", "ctrl+c":
				m.overview = false
				return m, nil
			case "b":
				m.backdrop = !m.backdrop
				return m, nil
			case "up", "k", "down", "j", "pgup", "pgdown":
				var cmd tea.Cmd
				m.tbl, cmd = m.tbl.Update(msg)
				return m, cmd
			case "enter", "right", "l":
				m.overview = false
				m.detail = true
				return m, m.loadCurrent()
			}
			return m, nil
		}
		if m.detail {
			switch msg.String() {
			case "q", "esc", "left", "h", "ctrl+c":
				m.detail = false
				return m, nil
			case "o", "enter":
				if w, ok := m.current(); ok {
					openBrowser(fmt.Sprintf(waterURL, w.ID))
					m.note = "✓ opened " + w.Name
				}
				return m, nil
			case "up", "k", "down", "j":
				var cmd tea.Cmd
				m.tbl, cmd = m.tbl.Update(msg)
				return m, tea.Batch(cmd, m.loadCurrent())
			case "b":
				m.backdrop = !m.backdrop
				return m, nil
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "enter", "right", "l":
			m.detail = true
			return m, m.loadCurrent()
		case "i":
			m.overview = true
			if m.ovGeoms == nil && !m.ovLoading {
				m.ovLoading = true
				w := float64((m.width-4)*2 - 1)
				h := float64((m.height-7)*4 - 1)
				return m, buildOverviewCmd(m.rows, w/h)
			}
			return m, nil
		case "o":
			if w, ok := m.current(); ok {
				openBrowser(fmt.Sprintf(waterURL, w.ID))
				m.note = "✓ opened " + w.Name
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

// overviewView draws the whole state: counties + every result stream + selection.
func (m browseModel) overviewView() string {
	mapW := maxi(40, m.width-4)
	mapH := maxi(12, m.height-7)
	selID := ""
	selName := ""
	if w, ok := m.current(); ok {
		selID, selName = w.ID, w.Name
	}

	var body string
	if m.ovGeoms == nil {
		if m.ovErr != "" {
			body = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff7aa8")).
				Render("overview failed: " + m.ovErr)
		} else {
			body = byeStyle.Render(fmt.Sprintf("… plotting %s waters across Idaho …", humanize(len(m.rows))))
		}
	} else {
		nx0, nx1, ny0, ny1 := m.ovBox[0], m.ovBox[1], m.ovBox[2], m.ovBox[3]
		w, h := mapW-4, mapH-2
		dotW, dotH := float64(w*2-1), float64(h*4-1)
		k := math.Min(dotW/(nx1-nx0), dotH/(ny1-ny0))
		cx := nx0 - (dotW/k-(nx1-nx0))/2
		cy := ny0 - (dotH/k-(ny1-ny0))/2
		toDot := func(p [2]float64) (int, int) {
			return int(math.Round((mercX(p[0]) - cx) * k)), int(math.Round((mercY(p[1]) - cy) * k))
		}
		drawAll := func(c *canvas, lines []polyline) {
			for _, l := range lines {
				for i := 1; i < len(l); i++ {
					x0, y0 := toDot(l[i-1])
					x1, y1 := toDot(l[i])
					c.line(x0, y0, x1, y1)
				}
			}
		}
		countyC := newCanvas(w, h)
		drawAll(countyC, countyLines())
		streamC := newCanvas(w, h)
		for id, lines := range m.ovGeoms {
			if id != selID {
				drawAll(streamC, lines)
			}
		}
		selC := newCanvas(w, h)
		if sel, okSel := m.ovGeoms[selID]; okSel {
			drawAll(selC, sel)
		}
		bgAt := func(cellX, cellY int) (int, int, int, bool) {
			if !m.backdrop || m.ovImg == nil {
				return 0, 0, 0, false
			}
			worldPx := float64(int(256) << m.ovZoom)
			nX := cx + (float64(cellX*2)+1)/k
			nY := cy + (float64(cellY*4)+2)/k
			ix := int(nX*worldPx) - m.ovPx0
			iy := int(nY*worldPx) - m.ovPy0
			if ix < 0 || iy < 0 || ix >= m.ovImg.Rect.Dx() || iy >= m.ovImg.Rect.Dy() {
				return 0, 0, 0, false
			}
			cr, cg, cb, _ := m.ovImg.At(ix, iy).RGBA()
			return int(float64(cr>>8) * 0.5), int(float64(cg>>8) * 0.5), int(float64(cb>>8) * 0.5), true
		}
		var b strings.Builder
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				sBits := selC.cells[y*selC.w+x]
				wBits := streamC.cells[y*streamC.w+x]
				cBits := countyC.cells[y*countyC.w+x]
				r, g, bl, hasBg := bgAt(x, y)
				if hasBg {
					b.WriteString(fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, bl))
				}
				switch {
				case sBits != 0:
					b.WriteString("\x1b[38;2;238;111;248m\x1b[1m") // pink selection
					b.WriteRune(0x2800 + (sBits | wBits))
					b.WriteString("\x1b[22m")
				case wBits != 0:
					b.WriteString("\x1b[38;2;83;214;240m") // cyan result streams
					b.WriteRune(0x2800 + wBits)
				case cBits != 0:
					b.WriteString("\x1b[38;2;122;116;160m") // muted county borders
					b.WriteRune(0x2800 + cBits)
				default:
					b.WriteRune(' ')
				}
				b.WriteString("\x1b[0m")
			}
			if y < h-1 {
				b.WriteRune('\n')
			}
		}
		body = b.String()
	}

	head := lipgloss.NewStyle().Bold(true).Foreground(cream).Background(purple).Padding(0, 2).
		Render("Idaho overview") +
		dimStyle.Render(fmt.Sprintf("  %s waters · counties · ", humanize(len(m.rows)))) +
		byeStyle.Render(selName)
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(purple).
		Padding(0, 1).Width(mapW).Render(body)
	help := dimStyle.Render("  ↑↓ pick water (pink) · ") +
		lipgloss.NewStyle().Foreground(pink).Render("enter") + dimStyle.Render(" water page · ") +
		lipgloss.NewStyle().Foreground(pink).Render("b") + dimStyle.Render(" backdrop · ") +
		lipgloss.NewStyle().Foreground(pink).Render("esc/i") + dimStyle.Render(" back")
	return head + "\n" + box + "\n" + help
}

// detailView recreates the IDFG water page inside the TUI: info + braille trace map.
func (m browseModel) detailView() string {
	w, ok := m.current()
	if !ok {
		return "no water selected"
	}
	infoW := 42
	mapW, mapH := m.mapDims()

	// info column
	name := w.Name
	if w.Var != nil && *w.Var != "" {
		name += " (" + *w.Var + ")"
	}
	typ, unit := "stream", " miles"
	if w.Layer != 1 {
		typ, unit = "lake", " acres"
	}
	var info strings.Builder
	info.WriteString(gradient(name, "#7d56f4", "#ee6ff8") + "\n")
	if w.Trib != "" {
		info.WriteString(dimStyle.Render(w.Trib) + "\n")
	}
	info.WriteString("\n")
	kv := func(k, v string) {
		info.WriteString(cardLabel.Render(k) + lipgloss.NewStyle().Foreground(cream).Render(v) + "\n")
	}
	kv("county", w.County)
	if w.Region != nil {
		kv("region", regionNames[*w.Region])
	}
	kv("type", typ)
	if w.Size != "" {
		kv("length", w.Size+unit)
	}
	info.WriteString("\n" + dimStyle.Render("species observed in surveys") + "\n")
	sp := make([]string, 0, len(w.Sp))
	for _, s := range w.Sp {
		sp = append(sp, m.spName[s])
	}
	sort.Strings(sp)
	for i, s := range sp {
		st := incPalette[i%len(incPalette)]
		info.WriteString(st.Render(s) + "\n")
	}
	info.WriteString("\n" + lipgloss.NewStyle().Foreground(pink).Underline(true).
		Render(fmt.Sprintf(waterURL, w.ID)))
	infoBox := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(purple).
		Padding(0, 2).Width(infoW).Render(info.String())

	// map column
	var mapBody string
	if md, have := m.geomCache[w.ID]; have {
		mapBody = renderMap(md, mapW-4, mapH-2, m.backdrop)
	} else if errStr, failed := m.geomErr[w.ID]; failed {
		mapBody = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff7aa8")).
			Render("could not trace this water\n" + errStr)
	} else {
		mapBody = byeStyle.Render("… tracing " + typ + " + surroundings from IDFG …")
	}
	mapBox := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(pink).
		Padding(0, 1).Width(mapW).Render(mapBody)
	mapTitle := dimStyle.Render("  topo · IDFG hydrography · N↑ · ") +
		lipgloss.NewStyle().Foreground(pink).Render("b") + dimStyle.Render(" backdrop on/off")

	head := lipgloss.NewStyle().Bold(true).Foreground(cream).Background(purple).Padding(0, 2).
		Render("Water page") + dimStyle.Render(fmt.Sprintf("  row %d/%d", m.tbl.Cursor()+1, len(m.rows)))
	body := lipgloss.JoinHorizontal(lipgloss.Top, infoBox, " ", lipgloss.JoinVertical(lipgloss.Left, mapBox, mapTitle))
	help := dimStyle.Render("  ↑↓ next water · ") +
		lipgloss.NewStyle().Foreground(pink).Render("enter/o") + dimStyle.Render(" open on IDFG · ") +
		lipgloss.NewStyle().Foreground(pink).Render("esc/←") + dimStyle.Render(" back to table")
	note := ""
	if m.note != "" {
		note = "   " + countStyle.Render(m.note)
	}
	return head + "\n" + body + "\n" + help + note
}

func (m browseModel) View() string {
	if m.overview {
		return m.overviewView()
	}
	if m.detail {
		return m.detailView()
	}
	head := lipgloss.NewStyle().Bold(true).Foreground(cream).Background(purple).Padding(0, 2).
		Render(fmt.Sprintf("Browsing %s waters", humanize(len(m.rows)))) +
		dimStyle.Render(fmt.Sprintf("  row %d/%d", m.tbl.Cursor()+1, len(m.rows)))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(purple).
		Padding(0, 1).Render(m.tbl.View())
	detail := dimStyle.Render("…")
	if c := m.tbl.Cursor(); c >= 0 && c < len(m.rows) {
		w := m.rows[c]
		sp := make([]string, 0, len(w.Sp))
		for _, s := range w.Sp {
			sp = append(sp, m.spName[s])
		}
		sort.Strings(sp)
		detail = lipgloss.NewStyle().Bold(true).Foreground(cream).Render(w.Name) +
			dimStyle.Render("  ·  "+w.Trib+"  ·  ") +
			lipgloss.NewStyle().Foreground(green).Render(strings.Join(sp, ", ")) + "\n" +
			lipgloss.NewStyle().Foreground(pink).Underline(true).Render(fmt.Sprintf(waterURL, w.ID))
	}
	note := ""
	if m.note != "" {
		note = "   " + countStyle.Render(m.note)
	}
	help := dimStyle.Render("  ↑↓/pgup/pgdn move · ") +
		lipgloss.NewStyle().Foreground(pink).Render("enter/→") + dimStyle.Render(" water page + map · ") +
		lipgloss.NewStyle().Foreground(pink).Render("i") + dimStyle.Render(" Idaho overview · ") +
		lipgloss.NewStyle().Foreground(pink).Render("o") + dimStyle.Render(" open on IDFG · ") +
		lipgloss.NewStyle().Foreground(pink).Render("q") + dimStyle.Render(" back") + note
	return head + "\n" + box + "\n" + detail + "\n" + help
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	default:
		exec.Command("xdg-open", url).Start()
	}
}

func exportCSV(rows []Water) (string, error) {
	f, err := os.Create("idaho-waters.csv")
	if err != nil {
		return "", err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{"name", "variant", "county", "drainage", "size", "url"})
	for _, r := range rows {
		v := ""
		if r.Var != nil {
			v = *r.Var
		}
		w.Write([]string{r.Name, v, r.County, r.Trib, r.Size, fmt.Sprintf(waterURL, r.ID)})
	}
	return f.Name(), nil
}

func main() {
	d, err := loadData()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// --maps: print both reference maps (embedded data only) and exit
	if len(os.Args) > 1 && os.Args[1] == "--maps" {
		ref := refMapModel{width: 150, height: 40}
		fmt.Println(ref.View())
		ref.mode = 1
		fmt.Println(ref.View())
		return
	}

	// --demo: run the canonical query non-interactively (used for testing)
	if len(os.Args) > 1 && os.Args[1] == "--demo" {
		inc := resolve(d, []string{"g:Any cutthroat"})
		exc := resolve(d, []string{"g:Brook trout (both)"})
		rows := query(d, inc, exc, 0, "Lemhi", "streams")
		fmt.Println(banner(d))
		fmt.Println(summaryCard(d, inc, exc, 0, "Lemhi", "streams"))
		fmt.Println(renderTable(d, rows, 10))
		fmt.Println(countStyle.Render(fmt.Sprintf("🎣 %d waters", len(rows))))
		fmt.Println()
		fmt.Println(histogram(d, rows))
		b := newBrowse(d, rows)
		b.width, b.height = 150, 26
		b.applySize()
		fmt.Println(b.View())
		for i, w := range b.rows {
			if w.Name == "Middle Fork Little Timber Creek" {
				b.tbl.SetCursor(i)
				break
			}
		}
		b.width, b.height = 150, 34
		if w, ok := b.current(); ok {
			if md, err := buildMapData(w, b.mapAspect()); err == nil {
				b.geomCache[w.ID] = md
			} else {
				b.geomErr[w.ID] = err.Error()
			}
		}
		b.detail = true
		fmt.Println(b.detailView())

		// statewide overview with all result streams + counties
		wOv := float64((b.width-4)*2 - 1)
		hOv := float64((b.height-7)*4 - 1)
		if msg, ok := buildOverviewCmd(b.rows, wOv/hOv)().(ovMsg); ok {
			b.ovGeoms, b.ovErr = msg.geoms, ""
			if msg.err != nil {
				b.ovErr = msg.err.Error()
			}
			b.ovImg, b.ovPx0, b.ovPy0, b.ovZoom = msg.img, msg.px0, msg.py0, msg.zoom
			b.ovBox = msg.box
		}
		b.detail = false
		b.overview = true
		fmt.Println(b.overviewView())

		// reference map, both modes (embedded data, no network)
		ref := refMapModel{width: 150, height: 40}
		fmt.Println(ref.View())
		ref.mode = 1
		fmt.Println(ref.View())
		return
	}

	fmt.Println(banner(d))

	for {
		var incPicks, excPicks []string
		var regionPick int
		var countyPick, bodyPick string

		countySet := map[string]bool{}
		for _, w := range d.Waters {
			for _, c := range strings.Split(w.County, ", ") {
				if c != "" {
					countySet[c] = true
				}
			}
		}
		counties := make([]string, 0, len(countySet))
		for c := range countySet {
			counties = append(counties, c)
		}
		sort.Strings(counties)
		countyOpts := []huh.Option[string]{huh.NewOption("Anywhere", "")}
		for _, c := range counties {
			countyOpts = append(countyOpts, huh.NewOption(c, c))
		}
		regionOpts := []huh.Option[int]{huh.NewOption("Anywhere in Idaho", 0)}
		for _, r := range []int{1, 2, 3, 8, 4, 5, 6, 7} {
			regionOpts = append(regionOpts, huh.NewOption(fmt.Sprintf("%d · %s", r, regionNames[r]), r))
		}

		// consistency with the single-selects: enter (or space) toggles a fish,
		// tab moves to the next question
		km := huh.NewDefaultKeyMap()
		km.MultiSelect.Toggle = key.NewBinding(
			key.WithKeys("enter", " ", "x"), key.WithHelp("enter", "toggle"))
		km.MultiSelect.Next = key.NewBinding(
			key.WithKeys("tab"), key.WithHelp("tab", "next question"))
		// huh swaps Next for Submit on a form's last field; without tab here the
		// final multiselect becomes a trap (enter belongs to Toggle now)
		km.MultiSelect.Submit = key.NewBinding(
			key.WithKeys("tab"), key.WithHelp("tab", "continue"))

		speciesForm := huh.NewForm(
			huh.NewGroup(
				huh.NewMultiSelect[string]().
					Title("What must be swimming there?").
					Description("enter toggles a fish · tab when done · ⭑ picks whole families").
					Options(speciesOptions(d)...).
					Height(14).
					Value(&incPicks),
				huh.NewMultiSelect[string]().
					Title("Anything that must NOT be there?").
					Description("enter toggles · tab when done").
					Options(speciesOptions(d)...).
					Height(10).
					Value(&excPicks),
			),
		).WithTheme(huh.ThemeCharm()).WithKeyMap(km)

		if err := speciesForm.Run(); err != nil {
			fmt.Println(byeStyle.Render("tight lines 🎣"))
			return
		}

		whereForm := huh.NewForm(huh.NewGroup(
			huh.NewSelect[int]().
				Title("Which region?").
				Options(regionOpts...).
				Value(&regionPick),
			huh.NewSelect[string]().
				Title("Which county?").
				Description("type / to filter").
				Options(countyOpts...).
				Height(10).
				Value(&countyPick),
			huh.NewSelect[string]().
				Title("What kind of water?").
				Options(
					huh.NewOption("Streams & rivers", "streams"),
					huh.NewOption("Lakes & reservoirs", "lakes"),
					huh.NewOption("Both", "both"),
				).
				Value(&bodyPick),
		)).WithTheme(huh.ThemeCharm())

		wm, err := tea.NewProgram(whereModel{form: whereForm, width: 100, height: 34}).Run()
		if err != nil {
			fmt.Println(byeStyle.Render("tight lines 🎣"))
			return
		}
		if fm, ok := wm.(whereModel); !ok || fm.form.State != huh.StateCompleted {
			fmt.Println(byeStyle.Render("tight lines 🎣"))
			return
		}

		inc, exc := resolve(d, incPicks), resolve(d, excPicks)
		var rows []Water
		_ = spinner.New().Title("sifting the waters…").Action(func() {
			rows = query(d, inc, exc, regionPick, countyPick, bodyPick)
		}).Run()

		bodyName := map[string]string{"streams": "streams & rivers", "lakes": "lakes & reservoirs", "both": "streams & lakes"}[bodyPick]
		fmt.Println(summaryCard(d, inc, exc, regionPick, countyPick, bodyName))
		fmt.Println(renderTable(d, rows, 20))
		extra := ""
		if len(rows) > 20 {
			extra = dimStyle.Render(fmt.Sprintf("  +%d more — export to see all", len(rows)-20))
		}
		fmt.Println(countStyle.Render(fmt.Sprintf("🎣 %d waters", len(rows))) + extra)
		fmt.Println()
		fmt.Println(histogram(d, rows))

		for {
			var act string
			err := huh.NewSelect[string]().
				Title("And now?").
				Options(
					huh.NewOption("Browse the full table (↑↓)", "browse"),
					huh.NewOption("Open a water on IDFG", "open"),
					huh.NewOption("Export CSV", "csv"),
					huh.NewOption("New search", "again"),
					huh.NewOption("Done", "done"),
				).Value(&act).WithTheme(huh.ThemeCharm()).Run()
			if err != nil || act == "done" {
				fmt.Println(byeStyle.Render("tight lines 🎣"))
				return
			}
			if act == "again" {
				break
			}
			if act == "browse" {
				if _, err := tea.NewProgram(newBrowse(d, rows), tea.WithAltScreen()).Run(); err != nil {
					fmt.Println(err)
				}
			}
			if act == "csv" {
				if name, err := exportCSV(rows); err == nil {
					fmt.Println(countStyle.Render(fmt.Sprintf("✓ %d waters → %s", len(rows), name)))
				}
			}
			if act == "open" {
				opts := []huh.Option[string]{}
				for i, w := range rows {
					if i >= 100 {
						break
					}
					opts = append(opts, huh.NewOption(w.Name+dimStyle.Render("  ·  "+w.Trib), w.ID))
				}
				var id string
				if err := huh.NewSelect[string]().Title("Which one?").Description("type / to filter").
					Options(opts...).Height(12).Value(&id).WithTheme(huh.ThemeCharm()).Run(); err == nil && id != "" {
					openBrowser(fmt.Sprintf(waterURL, id))
					fmt.Println(countStyle.Render("✓ opened in browser"))
				}
			}
		}
	}
}

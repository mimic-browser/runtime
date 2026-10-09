package cdp

import (
	"bytes"

	"encoding/base64"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"testing"
)

func TestInspectorBlinkScreencastLiveAndCleanup(t *testing.T) {
	executable, err := rendererExecutable("")
	if err != nil {
		t.Skip(err)
	}
	s, addr := runningServer(t)
	s.DevToolsChrome = executable
	_, err = evaluatePageFixture(s.Page, `document.body.innerHTML='<button id="probe" style="position:absolute;left:20px;top:20px;width:100px;height:50px;background:red">hello</button>';globalThis.clicks=0;document.querySelector('#probe').onclick=()=>{clicks++}`)
	if err != nil {
		t.Fatal(err)
	}
	w := newInspectorWire(t, addr)
	id := w.call(t, "", "Target.attachToTarget", map[string]any{"targetId": s.Page.ID, "flatten": true})["sessionId"].(string)
	w.call(t, id, "Log.enable", map[string]any{})
	w.call(t, id, "Page.enable", map[string]any{})
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})["root"]
	node := findInspectorNode(root, "id", "probe")
	w.call(t, id, "Page.startScreencast", map[string]any{"format": "png", "maxWidth": 400, "maxHeight": 300})
	readFrame := func() map[string]any { return w.event(t, "Page.screencastFrame") }
	frame := readFrame()
	raw, err := base64.StdEncoding.DecodeString(frame["data"].(string))
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() > 400 || img.Bounds().Dy() > 300 {
		t.Fatalf("max dimensions not respected: %v", img.Bounds())
	}
	w.call(t, id, "Page.screencastFrameAck", map[string]any{"sessionId": frame["sessionId"]})
	w.call(t, id, "Input.dispatchMouseEvent", map[string]any{"type": "mousePressed", "x": 40, "y": 40, "button": "left", "clickCount": 1})
	w.call(t, id, "Input.dispatchMouseEvent", map[string]any{"type": "mouseReleased", "x": 40, "y": 40, "button": "left", "clickCount": 1})
	value, err := evaluatePageFixture(s.Page, `clicks`)
	if err != nil || coordinateValue(value) != 1 {
		t.Fatalf("canonical click: %#v %v", value, err)
	}
	_, err = evaluatePageFixture(s.Page, `document.querySelector('#probe').style.background='blue'`)
	if err != nil {
		t.Fatal(err)
	}
	blue := false
	for attempt := 0; attempt < 5 && !blue; attempt++ {
		second := readFrame()
		data, err := base64.StdEncoding.DecodeString(second["data"].(string))
		if err != nil {
			t.Fatal(err)
		}
		image, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		deviceWidth := coordinateValue(second["metadata"].(map[string]any)["deviceWidth"])
		x := int(30 * float64(image.Bounds().Dx()) / deviceWidth)
		r, g, b, _ := image.At(x, x).RGBA()
		blue = b > 50000 && r < 10000 && g < 10000
		w.call(t, id, "Page.screencastFrameAck", map[string]any{"sessionId": second["sessionId"]})
	}
	if !blue {
		t.Fatal("latest canonical CSS edit was not rendered blue")
	}
	location := w.call(t, id, "DOM.getNodeForLocation", map[string]any{"x": 40, "y": 40})
	if location["backendNodeId"] != node["backendNodeId"] {
		t.Fatalf("presentation exposed renderer node identity: %v", location)
	}
	w.call(t, id, "Overlay.enable", map[string]any{})
	w.call(t, id, "Overlay.highlightNode", map[string]any{"nodeId": node["nodeId"], "highlightConfig": map[string]any{"contentColor": map[string]any{"r": 255, "g": 0, "b": 0, "a": 0.3}}})
	highlightFrame := readFrame()
	w.call(t, id, "Page.screencastFrameAck", map[string]any{"sessionId": highlightFrame["sessionId"]})
	w.call(t, id, "Overlay.setInspectMode", map[string]any{"mode": "searchForNode"})
	w.call(t, id, "Input.dispatchMouseEvent", map[string]any{"type": "mouseReleased", "x": 40, "y": 40, "button": "left"})
	inspected := w.event(t, "Overlay.inspectNodeRequested")
	if inspected["backendNodeId"] != node["backendNodeId"] {
		t.Fatalf("inspect picker: %v", inspected)
	}
	value, err = evaluatePageFixture(s.Page, `clicks`)
	if err != nil || coordinateValue(value) != 1 {
		t.Fatal("inspect-mode picking activated the author click handler")
	}
	w.call(t, id, "Overlay.hideHighlight", map[string]any{})
	w.call(t, id, "Page.disable", map[string]any{})
	w.call(t, id, "Page.stopScreencast", map[string]any{})
	s.Page.LockCommands()
	for _, client := range s.clientSnapshot() {
		for _, session := range client.snapshot() {
			if session.id == id && session.castState() != nil {
				t.Error("stopped cast retained renderer")
			}
		}
	}
	s.Page.UnlockCommands()

}

func TestInspectorBlinkWheelChangesVisiblePixels(t *testing.T) {
	executable, err := rendererExecutable("")
	if err != nil {
		t.Skip(err)
	}
	s, addr := runningServer(t)
	s.DevToolsChrome = executable
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.style.margin='0';document.body.innerHTML='<div style="height:200px;background:red"></div><div style="height:3000px;background:blue"></div>'`})
	w.call(t, id, "Page.enable", map[string]any{})
	w.call(t, id, "Page.startScreencast", map[string]any{"format": "png"})
	first := w.event(t, "Page.screencastFrame")
	w.call(t, id, "Page.screencastFrameAck", map[string]any{"sessionId": first["sessionId"]})
	w.call(t, id, "Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": 40, "y": 40, "deltaX": 0, "deltaY": 300})
	pos := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "scrollY", "returnByValue": true})["result"].(map[string]any)["value"]
	if coordinateValue(pos) != 300 {
		t.Fatalf("wheel did not update canonical scroll: %v", pos)
	}
	frame := w.event(t, "Page.screencastFrame")
	raw, err := base64.StdEncoding.DecodeString(frame["data"].(string))
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(40, 40).RGBA()
	if b < 50000 || r > 10000 || g > 10000 {
		t.Fatalf("view still captured pre-scroll document pixels: %v %v %v", r, g, b)
	}
}

func TestInspectorBlinkNestedScrollAndRasterResolution(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		t.Run(format, func(t *testing.T) {
			executable, err := rendererExecutable("")
			if err != nil {
				t.Skip(err)
			}
			s, addr := runningServer(t)
			s.DevToolsChrome = executable
			w, id := inspectorSession(t, s, addr)
			w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.style.margin='0';document.body.innerHTML='<div id="scroller" style="width:200px;height:150px;overflow:auto"><div style="height:200px;background:red"></div><div style="height:2000px;background:blue"></div></div>';globalThis.wheels=0;document.querySelector('#scroller').addEventListener('wheel',()=>wheels++)`})
			w.call(t, id, "Page.enable", map[string]any{})
			env := s.Page.Environment()
			width, height := env.Window.ViewportWidth, env.Window.ViewportHeight
			w.call(t, id, "Page.startScreencast", map[string]any{"format": format, "maxWidth": width * 2, "maxHeight": height * 2})
			first := w.event(t, "Page.screencastFrame")
			raw, _ := base64.StdEncoding.DecodeString(first["data"].(string))
			img, _, err := image.Decode(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds().Dx() != width*2 || img.Bounds().Dy() != height*2 {
				t.Fatalf("requested high-resolution raster lost: %v", img.Bounds())
			}
			w.call(t, id, "Page.screencastFrameAck", map[string]any{"sessionId": first["sessionId"]})
			w.call(t, id, "Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": 40, "y": 40, "deltaX": 0, "deltaY": 300})
			pos := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "({top:document.querySelector('#scroller').scrollTop,window:scrollY,wheels})", "returnByValue": true})["result"].(map[string]any)["value"].(map[string]any)
			if pos["top"] != float64(300) || pos["window"] != float64(0) || pos["wheels"] != float64(1) {
				t.Fatalf("nested wheel ownership: %v", pos)
			}
			frame := w.event(t, "Page.screencastFrame")
			raw, _ = base64.StdEncoding.DecodeString(frame["data"].(string))
			img, _, err = image.Decode(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			r, g, b, _ := img.At(80, 80).RGBA()
			if b < 50000 || r > 10000 || g > 10000 {
				t.Fatalf("nested scroll not rendered: %v %v %v", r, g, b)
			}
			w.call(t, id, "Page.screencastFrameAck", map[string]any{"sessionId": frame["sessionId"]})
			w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.querySelector('#scroller').addEventListener('wheel',event=>event.preventDefault(),{passive:false})`})
			w.call(t, id, "Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": 40, "y": 40, "deltaX": 0, "deltaY": 300})
			pos = w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "({top:document.querySelector('#scroller').scrollTop,wheels})", "returnByValue": true})["result"].(map[string]any)["value"].(map[string]any)
			if pos["top"] != float64(300) || pos["wheels"] != float64(2) {
				t.Fatalf("cancelled wheel scrolled: %v", pos)
			}

		})
	}
}

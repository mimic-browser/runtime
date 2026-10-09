package cdp

import (
	"context"
	"encoding/base64"
	"fmt"
	"unicode/utf8"

	"github.com/moreveal/mimic/internal/browser"
	"github.com/moreveal/mimic/internal/trace"
)

func (s *session) handlePage(ctx context.Context, method string, p map[string]any) (any, bool, error) {
	empty := map[string]any{}
	switch method {
	case "Page.getResourceTree":
		var tree func(*browser.Frame) map[string]any
		tree = func(frame *browser.Frame) map[string]any {
			resources := []any{}
			for _, resource := range s.page.Loader().RetainedResources(frame.ID, frame.RealmID(), frame.URL()) {
				resources = append(resources, resource)
			}
			result := map[string]any{"frame": s.framePayload(frame), "resources": resources}
			children := []any{}
			for _, child := range frame.Children() {
				children = append(children, tree(child))
			}
			if len(children) > 0 {
				result["childFrames"] = children
			}
			return result
		}
		return map[string]any{"frameTree": tree(s.page.Top)}, true, nil
	case "Page.getResourceContent":
		frame, ok := s.page.Frame(stringValue(p["frameId"]))
		if !ok {
			return nil, true, fmt.Errorf("No frame for given id found")
		}
		response, ok := s.page.Loader().CompletedResource(frame.ID, frame.RealmID(), frame.URL(), stringValue(p["url"]))
		if !ok {
			return nil, true, fmt.Errorf("No resource with given URL found")
		}
		content := string(response.Body)
		encoded := !utf8.Valid(response.Body)
		if encoded {
			content = base64.StdEncoding.EncodeToString(response.Body)
		}
		return map[string]any{"content": content, "base64Encoded": encoded}, true, nil
	case "Page.reload":
		return empty, true, s.page.Reload()
	case "Page.getNavigationHistory":
		index, entries := s.page.NavigationHistory()
		return map[string]any{"currentIndex": index, "entries": entries}, true, nil
	case "Page.navigateToHistoryEntry":
		return empty, true, s.page.NavigateToHistoryEntry(intValue(p["entryId"], 0))
	case "Page.setDocumentContent":
		return empty, true, s.page.SetDocumentContent(ctx, stringValue(p["frameId"]), stringValue(p["html"]))
	case "Page.removeScriptToEvaluateOnNewDocument":
		if !s.page.RemoveInitScript(stringValue(p["identifier"])) {
			return nil, true, fmt.Errorf("Script not found")
		}
		return empty, true, nil
	case "Page.getLayoutMetrics":
		value, err := s.page.EvaluateCommand(ctx, "", `(()=>{const w=innerWidth,h=innerHeight,d=document.documentElement,b=document.body;return {layoutViewport:{pageX:scrollX,pageY:scrollY,clientWidth:w,clientHeight:h},visualViewport:{offsetX:visualViewport.offsetLeft,offsetY:visualViewport.offsetTop,pageX:visualViewport.pageLeft,pageY:visualViewport.pageTop,clientWidth:visualViewport.width,clientHeight:visualViewport.height,scale:visualViewport.scale,zoom:1},contentSize:{x:0,y:0,width:Math.max(w,d?.scrollWidth||0,b?.scrollWidth||0),height:Math.max(h,d?.scrollHeight||0,b?.scrollHeight||0)}}})()`)
		if err != nil {
			return nil, true, err
		}
		metrics, ok := value.(map[string]any)
		if !ok {
			return nil, true, fmt.Errorf("Layout metrics unavailable")
		}
		for _, key := range []string{"layoutViewport", "visualViewport", "contentSize"} {
			css := "css" + string(key[0]-32) + key[1:]
			metrics[css] = metrics[key]
		}
		return metrics, true, nil
	case "Page.captureScreenshot":
		format := stringValue(p["format"])
		if format != "" && format != "png" {
			return nil, true, fmt.Errorf("approximate screenshots support PNG only")
		}
		if value, present := p["fromSurface"]; present && value == false {
			return nil, true, fmt.Errorf("view screenshots are unsupported")
		}
		if _, present := p["quality"]; present {
			return nil, true, fmt.Errorf("PNG screenshot quality is unsupported")
		}
		var clip *browser.ScreenshotClip
		if raw, present := p["clip"]; present {
			value, ok := raw.(map[string]any)
			if !ok {
				return nil, true, fmt.Errorf("invalid screenshot clip")
			}
			scale, _ := value["scale"].(float64)
			if scale != 1 {
				return nil, true, fmt.Errorf("scaled screenshots are unsupported")
			}
			clip = &browser.ScreenshotClip{}
			clip.X, _ = value["x"].(float64)
			clip.Y, _ = value["y"].(float64)
			clip.Width, _ = value["width"].(float64)
			clip.Height, _ = value["height"].(float64)
		}
		png, warnings, err := s.page.CaptureApproximateScreenshot(ctx, clip)
		if err != nil {
			return nil, true, err
		}
		for _, warning := range warnings {
			s.page.Trace().Add(trace.Error, "approximateScreenshot.warning", map[string]any{"message": warning})
		}
		return map[string]any{"data": png}, true, nil
	case "Page.bringToFront":
		return empty, true, nil
	case "Audits.enable", "WebMCP.enable":
		s.setDomain(method[:len(method)-7], true)
		return empty, true, nil
	case "Audits.disable", "WebMCP.disable":
		s.setDomain(method[:len(method)-8], false)
		return empty, true, nil
	}
	return nil, false, nil
}

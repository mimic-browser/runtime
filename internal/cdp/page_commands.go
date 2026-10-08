package cdp

import (
	"context"
	"fmt"
)

func (s *session) handlePage(ctx context.Context, method string, p map[string]any) (any, bool, error) {
	empty := map[string]any{}
	switch method {
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

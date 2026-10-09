package browser

import (
	"testing"
)

func TestURLBrowserParsingAndMutation(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, `JSON.stringify([
   new URL('http:\\\\www.google.com\\foo').href,
   new URL('http://foo:80').href,
   new URL('http://你好你好/').hostname,
   new URL('http://ExAmPlE.CoM/').href,
   new URL('../a b', 'https://EXAMPLE.com:443/dir/page').href,
   new URL('https://u:p@EXAMPLE.com:443/a').origin,
   new URL('data:text/plain,hi').origin,
   new URL('blob:https://EXAMPLE.com:443/id').origin
  ])`, `["http://www.google.com/foo","http://foo/","xn--6qqa088eba","http://example.com/","https://example.com/a%20b","https://example.com","null","https://example.com"]`)
		historyEval(t, p, `(()=>{
   const u=new URL('https://example.com/path?a=1');
   const params=u.searchParams;
   u.host='你好你好:443';u.pathname='a b';u.username='a@b';u.port='99999';
   u.search='?x=2';params.append('y','3');
   const before=u.href;let error='';
   try {u.href='/relative'} catch(e) {error=e.name}
   return [u.href,params===u.searchParams,error,u.href===before].join('|');
  })()`, `https://a%40b@xn--6qqa088eba/a%20b?x=2&y=3|true|TypeError|true`)
	})
}

"""Sequential safe disclosure/checkbox clicks with verified state and CDP timings.

Run against a freshly built Mimic executable or pinned Chrome 152. Profiles
are diagnostic; omit --profile-cdp for ordinary latency measurements.
"""
import argparse
import asyncio
import contextlib
import json
import subprocess
import time
from pathlib import Path

import live_latency as live


async def sample_resources(instance, result):
    while True:
        with contextlib.suppress(live.psutil.Error):
            root=live.psutil.Process(instance.process.pid)
            rss=private=cpu=0
            for process in [root,*root.children(recursive=True)]:
                with contextlib.suppress(live.psutil.Error):
                    memory=process.memory_info()
                    times=process.cpu_times()
                    rss+=memory.rss
                    private+=getattr(memory,'private',0)
                    cpu+=times.user+times.system
            result['resources'].append({'elapsed_ms':(time.perf_counter()-result['origin'])*1000,
                'tree_rss_bytes':rss,'tree_private_bytes':private,'live_tree_cpu_seconds':cpu})
        await asyncio.sleep(.1)


async def main(args):
    args.output = args.output.resolve()
    args.output.mkdir(parents=True, exist_ok=False)
    binary = args.binary.resolve()
    result = {'origin': time.perf_counter(), 'preview': False, 'phases': [],
              'commands': [], 'events': [], 'clicks': [], 'resources': [], 'binary': str(binary),
              'sha256': live.sha(binary), 'profile_cdp': args.profile_cdp,
              'harness_sha256':live.sha(Path(__file__)),
              'source': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=live.ROOT, text=True).strip(),
              'source_status': subprocess.check_output(['git', 'status', '--short'], cwd=live.ROOT, text=True),
              'url': ('https://demo.playwright.dev/todomvc/' if args.site=='todomvc'
                      else 'https://www.blast.hk/' if args.site=='blast'
                      else 'https://github.com/lightpanda-io/browser')}
    token = live.ACTIVE.set(result)
    instance = live.Instance(binary, result['sha256'], args.output/'process', args)
    sampler=None
    try:
        if args.chrome:
            endpoint = instance.endpoint
            instance.folder.mkdir()
            instance.log = (instance.folder/'process.log').open('wb')
            command = [str(binary), '--headless=new', '--no-first-run', '--no-default-browser-check',
                       '--remote-debugging-port='+endpoint.rsplit(':', 1)[1],
                       '--user-data-dir='+str(instance.folder/'profile'), 'about:blank']
            instance.process = subprocess.Popen(command, stdout=instance.log, stderr=subprocess.STDOUT,
                creationflags=subprocess.CREATE_NO_WINDOW)
            version = await live.endpoint_ready(endpoint, instance.process)
            result['version'] = version
            instance.browser = await live.connect(browserWSEndpoint=version['webSocketDebuggerUrl'], defaultViewport=None)
            page = await instance.browser.newPage()
        else:
            await instance.start(result)
            page = await instance.page(result)
        if args.profile_cdp and not args.chrome:
            await page._client.send('Mimic.startTrace')
        sampler=asyncio.create_task(sample_resources(instance,result))
        live.observe(page, result)
        await page.setViewport({'width': 1280, 'height': 900})
        await live.timed(result, 'navigate', page.goto(result['url'], waitUntil='domcontentloaded', timeout=60000))
        # Allow the site's asynchronously loaded interaction code to initialize.
        await asyncio.sleep(3)
        result['page'] = await page.evaluate('''() => ({title:document.title,url:location.href,
            elements:document.querySelectorAll('*').length})''')
        if args.site=='github-probe':
            result['observations']=[]
            for index in range(args.clicks):
                started=time.perf_counter()
                hit=await live.timed(result,'hit_'+str(index),page.evaluate('''() => {
                    const e=document.elementFromPoint(100,100);
                    return e?{tag:e.tagName,text:e.textContent.trim().slice(0,80)}:null;
                }'''))
                if not hit:
                    raise AssertionError('No hit target at the probe point')
                geometry_done=time.perf_counter()
                await live.timed(result,'move_'+str(index),page.mouse.move(100,100))
                result['observations'].append({'index':index,'hit':hit,
                    'hit_ms':(geometry_done-started)*1000,'move_ms':(time.perf_counter()-geometry_done)*1000})
            result['outcome']='all_observations_verified'
            return
        if args.site=='todomvc':
            await page.type('.new-todo','Mimic local latency measurement')
            await page.keyboard.press('Enter')
            button=await page.waitForSelector('.todo-list .toggle')
            read_state='e=>String(e.checked)'
            verify='(e,expected)=>String(e.checked)===expected'
        elif args.site=='blast':
            button=await page.waitForSelector('a.p-navgroup-link--logIn')
            read_state='e=>String(e.getAttribute("aria-expanded")==="true")'
            verify='(e,expected)=>String(e.getAttribute("aria-expanded")==="true")===expected'
        else:
            button = (await page.waitForFunction('''() => Array.from(document.querySelectorAll('summary')).find(
                e=>e.textContent.trim()==='Example Puppeteer script')''', timeout=30000)).asElement()
            read_state='e=>String(e.parentElement.open)'
            verify='(e,expected)=>String(e.parentElement.open)===expected'
        result['control'] = await page.evaluate('e=>e.outerHTML', button)
        if await page.evaluate(read_state, button) != 'false':
            raise AssertionError('Control did not start inactive')
        # Do not act if projected geometry would hit a different control.
        await button._scrollIntoViewIfNeeded()
        point=await button._clickablePoint()
        hit=await page.evaluate('(e,p)=>{const h=document.elementFromPoint(p.x,p.y);return {valid:h===e||e.contains(h),actual:h?.outerHTML.slice(0,500)}}',button,point)
        result['hit_check']=hit
        if not hit['valid']:
            raise AssertionError('Projected click hits a different control: '+str(hit))
        for index in range(args.clicks):
            expected = 'true' if index % 2 == 0 else 'false'
            started = time.perf_counter()
            operation='escape' if args.site=='blast' and index%2 else 'click'
            await live.timed(result, operation+'_'+str(index),
                page.keyboard.press('Escape') if operation=='escape' else button.click())
            dispatched = time.perf_counter()
            await live.timed(result, 'verify_'+str(index), page.waitForFunction(
                verify,
                {'timeout':15000}, button, expected))
            row={'index': index, 'operation':operation, 'expected_active': expected,
                'dispatch_ms': (dispatched-started)*1000,
                'verified_ms': (time.perf_counter()-started)*1000}
            result['clicks'].append(row)
            if args.exercise_form and expected=='true':
                field=await live.timed(result,'form_ready_'+str(index),page.waitForSelector(
                    'input[name="login"]',{'visible':True,'timeout':15000}))
                row['form_ready_ms']=(time.perf_counter()-started)*1000
                await field._scrollIntoViewIfNeeded()
                field_point=await field._clickablePoint()
                valid=await page.evaluate('(e,p)=>document.elementFromPoint(p.x,p.y)===e',field,field_point)
                if not valid:
                    raise AssertionError('Projected login-field hit is not the field')
                focused=time.perf_counter()
                await live.timed(result,'field_click_'+str(index),field.click())
                if not await page.evaluate('e=>document.activeElement===e',field):
                    raise AssertionError('Login field did not receive focus')
                row['field_click_ms']=(time.perf_counter()-focused)*1000
                editing=time.perf_counter()
                await page.keyboard.type('mimic-latency-check')
                if await page.evaluate('e=>e.value',field)!='mimic-latency-check':
                    raise AssertionError('Synthetic input text differs')
                await page.keyboard.down('Control')
                await page.keyboard.press('a')
                await page.keyboard.up('Control')
                await page.keyboard.press('Backspace')
                if await page.evaluate('e=>e.value',field)!='':
                    raise AssertionError('Synthetic input was not cleared')
                row['type_clear_ms']=(time.perf_counter()-editing)*1000
        result['outcome'] = 'all_clicks_verified'
    except Exception as error:
        result['outcome'] = 'error'
        result['error'] = repr(error)
        if args.site=='blast':
            with contextlib.suppress(Exception):
                result['failure_state']=await asyncio.wait_for(page.evaluate('''() => {
                    const a=document.querySelector('a.p-navgroup-link--logIn'),m=a?.nextElementSibling;
                    return {url:location.href,linkClass:a?.className,expanded:a?.getAttribute('aria-expanded'),
                        menuClass:m?.className,hidden:m?.getAttribute('aria-hidden'),
                        inputs:m?Array.from(m.querySelectorAll('input'),e=>({name:e.name,type:e.type})):[]};
                }'''),10)
    except asyncio.CancelledError:
        result['outcome']='timeout'
        raise
    finally:
        if sampler:
            sampler.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await sampler
        if args.profile_cdp and instance.browser:
            with contextlib.suppress(Exception):
                result['trace'] = await asyncio.wait_for(page._client.send('Mimic.getTrace'), 10)
        result['total_ms'] = (time.perf_counter()-result['origin'])*1000
        live.ACTIVE.reset(token)
        await instance.close()
        live.write(args.output/'result.json', result)
        print(json.dumps({k:result.get(k) for k in ('outcome','error','page','clicks','observations','failure_state')}, indent=2))
    if result['outcome'] != 'all_clicks_verified':
        raise SystemExit(1)


if __name__ == '__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--chrome', action='store_true')
    parser.add_argument('--profile-cdp', action='store_true')
    parser.add_argument('--clicks', type=int, default=6)
    parser.add_argument('--exercise-form',action='store_true',help='Blast only: verify form readiness, click, type synthetic text, then clear; never submit')
    parser.add_argument('--site', choices=['github','github-probe','todomvc','blast'], default='todomvc')
    parser.add_argument('--timeout',type=float,default=180)
    args=parser.parse_args()
    if args.clicks<1 or args.timeout<=0:
        parser.error('Positive click count and timeout are required')
    if args.exercise_form and args.site!='blast':
        parser.error('--exercise-form requires --site blast')
    args.env=[]
    args.navigation_timeout=40
    asyncio.run(asyncio.wait_for(main(args),args.timeout))

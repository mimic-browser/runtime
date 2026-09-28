"""Paired live latency diagnostics; does not modify the frozen benchmark or user script.

Example: python tools/performance/live_latency.py --before .build/before.exe
  --after .build/after.exe --output .build/live-latency-UNIQUE
Use --sites geometry --rounds 1 --budget 150 for 30 measured local rounds;
--sites geometry-stress --rounds 1 measures one cold all-200-row read.
--mode retained keeps a context between repetitions of one site. --preview on
loads the actual preview in pinned Chrome. --event-trace and --profile-cdp are
diagnostic runs, kept separate from ordinary performance results.
Requires aiohttp, pyppeteer and psutil. A timed-out trial is terminated, never
followed by another evaluate in the same process. Server timing is optional;
client command latency alone cannot distinguish network, queues and execution.
"""
import argparse
import asyncio
import contextlib
import contextvars
import hashlib
import json
import math
import os
from pathlib import Path
import socket
import statistics
import subprocess
import sys
import time
from datetime import datetime, timezone

import aiohttp
import psutil
from pyppeteer import connect
from pyppeteer.connection import Connection, CDPSession

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'tools/compatibility'))
from live_browser_comparison import SITES, CHROME, probe_expression, challenge

USER_SCRIPT = Path('C:/Users/moreveal/Desktop/test-script/main.py')
LOCALE = {'languages': ['en-US', 'en'], 'timezone': 'America/New_York',
          'intlLocale': 'en-US', 'reduceAcceptLanguage': False}
ACTIVE = contextvars.ContextVar('live_latency_record', default=None)
PHASE = contextvars.ContextVar('live_latency_phase', default='setup')


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2), encoding='utf-8')


def port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def stop(process):
    if process is None or process.poll() is not None:
        return
    with contextlib.suppress(psutil.Error):
        root = psutil.Process(process.pid)
        for child in root.children(recursive=True):
            with contextlib.suppress(psutil.Error):
                child.kill()
        root.kill()
    with contextlib.suppress(Exception):
        process.wait(timeout=5)


def trace_send(original):
    def send(client, method, params=None):
        recorder = ACTIVE.get()
        if recorder is None:
            return original(client, method, params)
        start = time.perf_counter()
        command = {'method': method, 'phase': PHASE.get(),
                   'start_ms': (start-recorder['origin'])*1000,
                   'session': getattr(client, '_sessionId', None)}
        if method == 'Input.dispatchMouseEvent':
            command['input_type'] = (params or {}).get('type')
        if method in ('Runtime.evaluate', 'Runtime.callFunctionOn'):
            expression = (params or {}).get('expression', (params or {}).get('functionDeclaration', ''))
            command['expression_sha256'] = hashlib.sha256(expression.encode()).hexdigest()
            command['await_promise'] = (params or {}).get('awaitPromise', False)
        recorder['commands'].append(command)
        try:
            future = original(client, method, params)
            command['id'] = getattr(client, '_lastId', None)
        except Exception as exc:
            command.update(duration_ms=(time.perf_counter()-start)*1000, error=str(exc))
            raise
        def done(future):
            command['duration_ms'] = (time.perf_counter()-start)*1000
            if future.cancelled():
                command['cancelled'] = True
            else:
                error = future.exception()
                if error is not None:
                    command['error'] = str(error)[:1000]
        future.add_done_callback(done)
        return future
    return send


Connection.send = trace_send(Connection.send)
CDPSession.send = trace_send(CDPSession.send)


async def timed(result, name, awaitable):
    start = time.perf_counter()
    stamp = {'name': name, 'start_ms': (start-result['origin'])*1000}
    result['phases'].append(stamp)
    token = PHASE.set(name)
    try:
        return await (awaitable() if callable(awaitable) else awaitable)
    except BaseException as exc:
        stamp['error'] = type(exc).__name__ + ': ' + str(exc)[:1000]
        raise
    finally:
        stamp['duration_ms'] = (time.perf_counter()-start)*1000
        PHASE.reset(token)


async def endpoint_ready(endpoint, process, attempts=None):
    deadline = time.perf_counter()+20
    async with aiohttp.ClientSession() as session:
        while time.perf_counter() < deadline:
            attempt_start = time.perf_counter()
            try:
                async with session.get(endpoint+'/json/version', timeout=aiohttp.ClientTimeout(total=.075,connect=.025,sock_connect=.025)) as response:
                    response.raise_for_status()
                    info = await response.json()
                if attempts is not None:
                    attempts.append({'duration_ms':(time.perf_counter()-attempt_start)*1000,'ready':True})
                return info
            except (aiohttp.ClientError, asyncio.TimeoutError) as exc:
                if attempts is not None:
                    attempts.append({'duration_ms':(time.perf_counter()-attempt_start)*1000,'ready':False,'error':type(exc).__name__})
                if process.poll() is not None:
                    raise RuntimeError(f'Browser exited: {process.returncode}')
                await asyncio.sleep(.01)
    raise TimeoutError('CDP startup exceeded 20 seconds')


class Instance:
    def __init__(self, binary, expected_hash, folder, args):
        self.binary, self.expected_hash, self.folder, self.args = binary, expected_hash, folder, args
        self.process = self.browser = self.log = self.context = None
        self.endpoint = f'http://127.0.0.1:{port()}'

    async def start(self, result):
        if sha(self.binary) != self.expected_hash:
            raise RuntimeError('Executable hash changed before launch')
        self.folder.mkdir(parents=True, exist_ok=True)
        self.log = (self.folder/'process.log').open('wb')
        env = os.environ.copy()
        for pair in self.args.env:
            key, value = pair.split('=', 1)
            env[key] = value
        if self.args.profile_cdp:
            env['MIMIC_PROFILE_CDP'] = '1'
        command = [str(self.binary), '-listen', self.endpoint.removeprefix('http://'),
                   '-navigation-timeout', f'{self.args.navigation_timeout}s']
        if result['preview']:
            command.append('-dev-preview')
        result['command'] = command
        self.process = subprocess.Popen(command, cwd=ROOT, env=env, stdout=self.log, stderr=subprocess.STDOUT,
                                        creationflags=subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0)
        result['startup_probes'] = []
        result['version'] = await timed(result, 'process_startup', endpoint_ready(self.endpoint,self.process,result['startup_probes']))
        self.browser = await timed(result, 'connect', connect(browserWSEndpoint=result['version']['webSocketDebuggerUrl'], defaultViewport=None))

    async def page(self, result):
        send = self.browser._connection.send
        if self.context is None:
            schema = await send('Mimic.getProfileSchema', {})
            profile = {'schemaVersion': 1, 'baseProfile': schema['baseProfiles'][0], 'locale': LOCALE.copy()}
            await send('Mimic.validateProfile', {'profile': profile})
            created = await send('Mimic.createContext', {'profile': profile, 'disposeOnDetach': True})
            self.context = created['browserContextId']
        result['context_id'] = self.context
        page = await self.browser._createPageInContext(self.context)
        result['effective_profile'] = await send('Mimic.getProfile', {'browserContextId': self.context})
        return page

    async def close(self):
        stop(self.process)
        if self.browser:
            with contextlib.suppress(Exception):
                await asyncio.wait_for(self.browser.disconnect(), 3)
        if self.log:
            self.log.close()


class Preview:
    def __init__(self):
        self.process = self.browser = self.log = None

    async def start(self, target_url, folder):
        endpoint = f'http://127.0.0.1:{port()}'
        self.log = (folder/'preview-chrome.log').open('wb')
        self.process = subprocess.Popen([str(CHROME), '--headless=new', '--no-first-run', '--no-default-browser-check',
            f'--remote-debugging-port={endpoint.rsplit(":", 1)[1]}', f'--user-data-dir={folder / "preview-profile"}', 'about:blank'],
            stdout=self.log, stderr=subprocess.STDOUT, creationflags=subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0)
        info = await endpoint_ready(endpoint, self.process)
        self.browser = await connect(browserWSEndpoint=info['webSocketDebuggerUrl'], defaultViewport=None)
        page = await self.browser.newPage()
        await page.goto(target_url, waitUntil='domcontentloaded')
        try:
            await page.waitForFunction("() => {const status=document.querySelector('#status');return status && status.textContent !== 'Connecting…' && status.textContent !== 'No targets';}", timeout=10000)
        finally:
            write(folder/'preview-state.json',await page.evaluate("() => ({url:location.href,title:document.title,body:document.body?.innerText.slice(0,1000)})"))

    async def close(self):
        stop(self.process)
        if self.browser:
            with contextlib.suppress(Exception):
                await asyncio.wait_for(self.browser.disconnect(), 3)
        if self.log:
            self.log.close()


def observe(page, result):
    def event(method, params):
        entry = {'method': method, 'elapsed_ms': (time.perf_counter()-result['origin'])*1000}
        if method == 'Network.requestWillBeSent':
            request = params.get('request', {})
            entry.update(requestId=params.get('requestId'), url=request.get('url'), type=params.get('type'))
        elif method == 'Network.responseReceived':
            response = params.get('response', {})
            headers = response.get('headers', {})
            entry.update(requestId=params.get('requestId'), url=response.get('url'), status=response.get('status'),
                         type=params.get('type'), timing=response.get('timing'),
                         fromDiskCache=response.get('fromDiskCache'),
                         cf_mitigated=next((v for k,v in headers.items() if k.lower() == 'cf-mitigated'), None))
        elif method == 'Network.loadingFailed':
            entry.update(requestId=params.get('requestId'), errorText=params.get('errorText'))
        elif method == 'Runtime.exceptionThrown':
            details = params.get('exceptionDetails', {})
            entry['error'] = (details.get('exception', {}).get('description') or details.get('text', ''))[:1500]
            entry['executionContextId'] = details.get('executionContextId')
            entry['url'] = details.get('url')
        else:
            entry['timestamp'] = params.get('timestamp')
        result['events'].append(entry)
    for method in ('Page.domContentEventFired', 'Page.loadEventFired', 'Network.requestWillBeSent',
                   'Network.responseReceived', 'Network.loadingFailed', 'Runtime.exceptionThrown'):
        page._client.on(method, lambda params, method=method: event(method, params))


def initialization(result):
    """Content can survive an aborted DCL handler; never equate it with readiness."""
    nav = next((p for p in result['phases'] if p['name']=='navigate_dcl'),None)
    if nav is None:
        return {'status':'not_started','valid':False,'dcl_ms':None,'errors':[]}
    start = nav['start_ms']
    # Exclude initial about:blank notifications and, for same-URL challenges,
    # the DCL belonging to an earlier challenge document.
    final_url = result['samples'][-1]['url'] if result['samples'] else result['url']
    documents = [e for e in result['events'] if e['method']=='Network.responseReceived'
                 and e.get('type')=='Document' and e['elapsed_ms']>=start
                 and e.get('url','').split('#')[0]==final_url.split('#')[0]]
    boundary = documents[-1]['elapsed_ms'] if documents else start
    dcl = next((e['elapsed_ms']-start for e in result['events']
                if e['method']=='Page.domContentEventFired' and e['elapsed_ms']>=boundary),None)
    errors = [{'source':'javascript','elapsed_ms':e['elapsed_ms']-start,
               'error':e['error'],'executionContextId':e.get('executionContextId'),'url':e.get('url')}
              for e in result['events'] if e['method']=='Runtime.exceptionThrown' and e['elapsed_ms']>=start]
    deadline_errors = [e for e in errors if any(text in e['error'].lower()
        for text in ('deadline exceeded','execution timed out','script execution timed out'))]
    if nav.get('error'):
        errors.append({'source':'navigation_wait','error':nav['error']})
    if deadline_errors:
        status = 'execution_deadline'
    elif nav.get('error'):
        status = 'cancelled' if nav['error'].startswith('CancelledError') else 'navigation_failed'
    elif 'duration_ms' not in nav:
        status = 'pending'
    elif dcl is None:
        status = 'dcl_missing'
    else:
        status = 'succeeded'
    return {'status':status,'valid':status=='succeeded','dcl_ms':dcl,
            'navigation_wait_ms':nav.get('duration_ms'),'deadline_errors':deadline_errors,'errors':errors}


async def chatgpt(page, result, args):
    await timed(result, 'navigate_dcl', page.goto('https://chatgpt.com/', waitUntil='domcontentloaded', timeout=args.budget*1000))
    result['observed_locale'] = await timed(result, 'locale_read', page.evaluate('''() => ({language:navigator.language,
        languages:Array.from(navigator.languages),intl:Intl.DateTimeFormat().resolvedOptions(),timezoneOffsetMinutes:new Date().getTimezoneOffset()})'''))
    handle = await timed(result, 'visible_login_search', lambda: page.waitForFunction('''selector => Array.from(document.querySelectorAll(selector)).find(el => {
        const box=el.getBoundingClientRect(); return box.width>0 && box.height>0 && !el.disabled && getComputedStyle(el).visibility!=='hidden';
    })''', {'timeout': 30000}, 'button[data-mobile-auth-entry-action="login"]'))
    button = handle.asElement()
    if button is None:
        raise AssertionError('Visible login result is not an element')
    dialog_id = await timed(result, 'dialog_id', page.evaluate("el => el.getAttribute('commandfor')", button))
    result['login_found_ms'] = (time.perf_counter()-result['origin'])*1000
    # Observe event boundaries without replacing dispatch or handlers. Separate
    # diagnostic mode because even a MutationObserver can perturb task ordering.
    if args.event_trace:
        await timed(result, 'install_event_trace', page.evaluate('''id => {
          const rows=globalThis.__mimicLatencyEvents=[];
          for(const kind of ['pointerdown','mousedown','pointerup','mouseup','click'])
            for(const capture of [true,false]) document.addEventListener(kind,e=>rows.push({kind,capture,time:performance.now()}),capture);
          const dialog=document.getElementById(id);
          if(dialog)new MutationObserver(()=>rows.push({kind:'dialog_mutation',time:performance.now(),open:dialog.open,modal:dialog.matches(':modal')})).observe(dialog,{attributes:true,attributeFilter:['open']});
        }''', dialog_id))
    scroll, point = button._scrollIntoViewIfNeeded, button._clickablePoint
    async def timed_scroll():
        return await timed(result, 'click_intersection_observer_scroll', scroll())
    async def timed_point():
        return await timed(result, 'click_content_quads', point())
    button._scrollIntoViewIfNeeded, button._clickablePoint = timed_scroll, timed_point
    await timed(result, 'button_click', button.click())
    await timed(result, 'dialog_observed_modal', lambda: page.waitForFunction(
        "id => {const dialog=document.getElementById(id);return dialog && dialog.open && dialog.matches(':modal')}",
        {'timeout':10000}, dialog_id))
    result['dialog_open_ms'] = (time.perf_counter()-result['origin'])*1000
    result['outcome'] = 'dialog_open_modal'
    if args.event_trace:
        result['click_events'] = await timed(result, 'event_trace_read', page.evaluate('globalThis.__mimicLatencyEvents'))


async def content(page, site, result, args):
    name, url, selector, minimum = site
    nav_start = time.perf_counter()
    result['navigation_start_ms'] = (nav_start-result['origin'])*1000
    # goto's lifecycle wait may coexist with one probe; there is never more than
    # one outstanding evaluate, and the outer budget terminates the whole trial.
    nav = asyncio.create_task(timed(result, 'navigate_dcl', page.goto(url, waitUntil='domcontentloaded', timeout=args.budget*1000)))
    challenge_seen = False
    try:
        while True:
            await asyncio.sleep(.25)
            reply = await timed(result, 'content_probe', page._client.send('Runtime.evaluate', {
                'expression':probe_expression(selector),'returnByValue':True}))
            if 'exceptionDetails' in reply:
                raise RuntimeError(str(reply['exceptionDetails'])[:1500])
            state = reply.get('result',{}).get('value')
            if not isinstance(state, dict):
                raise AssertionError('Missing by-value DOM snapshot')
            state['elapsed_ms'] = (time.perf_counter()-nav_start)*1000
            documents = [e for e in result['events'] if e['method']=='Network.responseReceived' and e.get('type')=='Document' and e.get('url','').split('#')[0]==state['url'].split('#')[0]]
            response = documents[-1] if documents else {}
            state['status'] = response.get('status')
            state['challenge'] = challenge(state) or response.get('cf_mitigated')=='challenge'
            challenge_seen |= state['challenge']
            result['samples'].append(state)
            good = state['url']!='about:blank' and state['selectorCount']>0 and state['textLength']>=minimum and not state['challenge'] and (state['status'] is None or state['status']<400)
            if good and result.get('first_content_ms') is None:
                result['first_content_ms'] = state['elapsed_ms']
            init = initialization(result)
            if init['status'] in ('execution_deadline','navigation_failed','cancelled'):
                result['outcome'] = 'content_after_initialization_failure' if result.get('first_content_ms') is not None else 'initialization_failed'
                break
            if good and state['elapsed_ms'] >= result['first_content_ms']+3000:
                result['content_reconfirmed_ms'] = state['elapsed_ms']
                if init['valid']:
                    result['outcome'] = 'content_after_challenge' if challenge_seen else 'content_observed'
                    break
                if init['status']=='dcl_missing':
                    result['outcome'] = 'content_initialization_unconfirmed'
                    break
            if state['status'] and state['status']>=400 and not state['challenge'] and state['elapsed_ms']>6000:
                result['outcome'] = 'http_error'
                break
        result['challenge_seen'] = challenge_seen
        if name == 'todomvc' and result['outcome']=='content_observed':
            async def todo():
                await page._client.send('Runtime.evaluate', {'expression':"document.querySelector('.new-todo').focus()"})
                await page._client.send('Input.insertText', {'text':'Mimic latency local test'})
                await page._client.send('Input.dispatchKeyEvent', {'type':'keyDown','key':'Enter','code':'Enter','windowsVirtualKeyCode':13,'nativeVirtualKeyCode':13,'text':'\r'})
                await page._client.send('Input.dispatchKeyEvent', {'type':'keyUp','key':'Enter','code':'Enter','windowsVirtualKeyCode':13,'nativeVirtualKeyCode':13})
                await page.waitForFunction("document.querySelector('.todo-list')?.textContent.includes('Mimic latency local test')",timeout=10000)
                return await page.evaluate("({count:document.querySelectorAll('.todo-list li').length,text:document.querySelector('.todo-list').textContent})")
            result['interaction'] = await timed(result, 'todo_create', todo())
            if result['interaction']['count'] != 1:
                raise AssertionError('TodoMVC did not create exactly one item')
    finally:
        if not nav.done():
            nav.cancel()
        with contextlib.suppress(Exception, asyncio.CancelledError):
            await nav


async def sample_memory(instance, result):
    while True:
        with contextlib.suppress(psutil.Error):
            process = psutil.Process(instance.process.pid)
            cpu = process.cpu_times()
            result['resources'].append({'elapsed_ms':(time.perf_counter()-result['origin'])*1000,
                'rss':process.memory_info().rss,'cpu_user_s':cpu.user,'cpu_system_s':cpu.system})
        await asyncio.sleep(.1)


async def geometry(page, result, stress=False):
    """Controlled broad reads and immediate mutation visibility, plus real clicks."""
    css = ''.join(f'main > .row[data-index="{i}"] span {{ color: rgb({i%255},0,0) }}' for i in range(80))
    html = '<!doctype html><style>' + css + '''
      .row { width:120px; height:12px; margin:0; padding:0; border:0; }
      .row.wide { width:180px; }
      #action { width:100px; height:30px; }
      </style><button id="action" onclick="globalThis.clicks=(globalThis.clicks||0)+1">Click</button><main>'''
    html += ''.join(f'<div class="row" data-index="{i}"><span>Row {i}</span></div>' for i in range(200)) + '</main>'
    await timed(result, 'fixture_setup', page.setContent(html))
    button = await page.querySelector('#action')
    rows = result['geometry_rows'] = []
    operations = {
        'geometry_read': ('''() => {
          let count=0,width=0,height=0,styledWidth=0;
          for(const node of document.querySelectorAll('.row')) {
            const box=node.getBoundingClientRect(); count++; width+=box.width; height+=box.height;
            styledWidth+=parseFloat(getComputedStyle(node).width);
          } return [count,width,height,styledWidth];
        }''', [200,24000,2400,24000]),
        'geometry_mutation': ('''() => {
          const node=document.querySelector('.row'); const before=node.getBoundingClientRect().width;
          node.classList.add('wide'); const during=node.getBoundingClientRect().width;
          const styled=parseFloat(getComputedStyle(node).width);
          node.classList.remove('wide'); const after=node.getBoundingClientRect().width;
          node.style.width='150px'; const inline=node.getBoundingClientRect().width;
          node.style.removeProperty('width'); const restored=node.getBoundingClientRect().width;
          return [before,during,styled,after,inline,restored];
        }''', [120,180,180,120,150,120]),
    }
    result['geometry_protocol'] = ('One cold read of all 200 rows; stress, not the 30-sample control.' if stress else
        '200 rows retained; read indices 0,67,133,199; one excluded warmup and 30 measured read/mutation/real-click rounds.')
    if not stress:
        expression,_ = operations['geometry_read']
        operations['geometry_read'] = (expression.replace("document.querySelectorAll('.row')",
            "[0,67,133,199].map(i=>document.querySelectorAll('.row')[i])"),[4,480,48,480])
    else:
        operations = {'geometry_read':operations['geometry_read']}
    for iteration in range(1 if stress else 31):
        for name,(expression,expected) in operations.items():
            start = time.perf_counter()
            value = await timed(result,name,page.evaluate(expression))
            row = {'name':name,'iteration':iteration,'warmup':iteration==0 and not stress,
                   'duration_ms':(time.perf_counter()-start)*1000,'value':value,'expected':expected}
            rows.append(row)
            if value != expected:
                raise AssertionError(f'{name}: {value} != {expected}')
        if stress:
            continue
        start = time.perf_counter()
        await timed(result,'geometry_click',button.click())
        elapsed = (time.perf_counter()-start)*1000
        clicks = await page.evaluate('globalThis.clicks')
        rows.append({'name':'geometry_click','iteration':iteration,'warmup':iteration==0,
                     'duration_ms':elapsed,'value':clicks,'expected':iteration+1})
        if clicks != iteration+1:
            raise AssertionError('Real click did not invoke the expected handler exactly once')
    result['geometry_summary'] = {}
    for name in (*operations,*(() if stress else ('geometry_click',))):
        values = sorted(row['duration_ms'] for row in rows if row['name']==name and not row['warmup'])
        result['geometry_summary'][name] = {'n':len(values),'median':statistics.median(values),
            'p95_nearest_rank':values[math.ceil(.95*len(values))-1],'min':values[0],'max':values[-1]}
    result['outcome'] = 'geometry_valid'


async def trial(instance, site, variant, repetition, mode, preview_on, folder, args, reused):
    folder.mkdir(parents=True)
    result = {'origin':time.perf_counter(),'started_utc':datetime.now(timezone.utc).isoformat(),
        'variant':variant,'site':site[0],'url':site[1],'repetition':repetition,'mode':mode,'preview':preview_on,
        'reused_process_and_context':reused,'sha256':instance.expected_hash,'binary':str(instance.binary),
        'process_log':str(instance.folder/'process.log'),
        'budget_s':args.budget,'event_trace_enabled':args.event_trace,
        'commands':[],'phases':[],'events':[],'samples':[],'resources':[],'outcome':'error'}
    token = ACTIVE.set(result)
    preview, page, sampler = Preview(), None, None
    async def perform():
        nonlocal page, sampler
        if not reused:
            await instance.start(result)
        sampler = asyncio.create_task(sample_memory(instance,result))
        page = await timed(result,'context_and_page',instance.page(result))
        if args.profile_cdp:
            await page._client.send('Mimic.startTrace',{})
        observe(page,result)
        if preview_on:
            await timed(result,'preview_open',preview.start(instance.endpoint+'/debug/preview/?target='+page.target._targetId,folder))
        result['scenario_start_ms'] = (time.perf_counter()-result['origin'])*1000
        if site[0] in ('geometry','geometry-stress'):
            await geometry(page,result,site[0]=='geometry-stress')
        elif site[0]=='chatgpt':
            await chatgpt(page,result,args)
        else:
            await content(page,site,result,args)
        if args.profile_cdp:
            result['server_trace'] = await timed(result,'server_trace',page._client.send('Mimic.getTrace',{}))
    try:
        await asyncio.wait_for(perform(),args.budget)
    except Exception as exc:
        result['error'] = type(exc).__name__+': '+str(exc)[:2000]
        result['outcome'] = 'trial_timeout' if isinstance(exc,asyncio.TimeoutError) else 'error'
    finally:
        result['total_ms'] = (time.perf_counter()-result['origin'])*1000
        if args.profile_cdp and page and 'server_trace' not in result:
            # This is a control-plane trace read, never another DOM evaluation
            # behind the timed-out command. Failure is followed by process kill.
            with contextlib.suppress(Exception):
                result['server_trace'] = await asyncio.wait_for(page._client.send('Mimic.getTrace',{}),2)
        if sampler:
            sampler.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await sampler
        await preview.close()
        if result['outcome'] in ('error','trial_timeout') or mode=='clean':
            await instance.close()
        elif page:
            try:
                await asyncio.wait_for(timed(result,'page_teardown',page.close()),5)
                await asyncio.sleep(.25)
                result['rss_recovered'] = psutil.Process(instance.process.pid).memory_info().rss
            except Exception as exc:
                result['teardown_error'] = str(exc)
                await instance.close()
        await asyncio.sleep(0)
        result['initialization'] = initialization(result)
        result['dcl_ms'] = result['initialization']['dcl_ms']
        result['valid_latency'] = result['outcome']=='geometry_valid' or (
            result['outcome'] in ('content_observed','content_after_challenge','dialog_open_modal')
            and result['initialization']['valid'])
        if result['outcome'] in ('content_observed','content_after_challenge') and not result['valid_latency']:
            result['outcome'] = 'content_initialization_unconfirmed'
        if result.get('dialog_open_ms') is not None:
            click = next(p for p in result['phases'] if p['name']=='button_click')
            result['click_to_modal_ms'] = result['dialog_open_ms']-click['start_ms']
        ACTIVE.reset(token)
        result.pop('origin')
        write(folder/'result.json',result)
    brief = {key:result.get(key) for key in ('site','variant','repetition','mode','preview','outcome','first_content_ms','login_found_ms','dialog_open_ms','total_ms','error')}
    print(json.dumps(brief,ensure_ascii=False),flush=True)
    return result


def summarize(results):
    groups = {}
    for result in results:
        key = '/'.join(str(result[k]) for k in ('site','variant','mode','preview'))
        groups.setdefault(key,[]).append(result)
    summary = {}
    for key,rows in groups.items():
        valid = [r for r in rows if r.get('valid_latency',False)]
        failed = [r for r in rows if not r.get('valid_latency',False)]
        entry = {'trials':len(rows),'valid_trials':len(valid),'failed_trials':len(failed),
                 'outcomes':[r['outcome'] for r in rows],'failure_metrics':{}}
        for selected,target in ((valid,entry),(failed,entry['failure_metrics'])):
            for metric in ('first_content_ms','dcl_ms','login_found_ms','dialog_open_ms','click_to_modal_ms','total_ms'):
                values = sorted(r[metric] for r in selected if r.get(metric) is not None)
                if values:
                    target[metric] = {'n':len(values),'median':statistics.median(values),'min':values[0],'max':values[-1],
                                     'p95_nearest_rank':values[math.ceil(.95*len(values))-1]}
            names = sorted({p['name'] for r in selected for p in r['phases']})
            target['phases_median_ms'] = {name:statistics.median(p['duration_ms'] for r in selected for p in r['phases'] if p['name']==name and 'error' not in p) for name in names if any(p['name']==name and 'error' not in p for r in selected for p in r['phases'])}
        summary[key] = entry
    return summary


async def main(args):
    out = args.output.resolve()
    if out.exists():
        raise FileExistsError('Use a new output directory to preserve evidence: '+str(out))
    binaries = {'before':args.before.resolve()}
    if args.after:
        binaries['after'] = args.after.resolve()
    hashes = {key:sha(binary) for key,binary in binaries.items()}
    sites = {s[0]:s for s in [('chatgpt','https://chatgpt.com/','',0),('geometry','about:blank','',0),('geometry-stress','about:blank','',0),*SITES]}
    selected = [sites[name] for name in args.sites.split(',')]
    out.mkdir(parents=True)
    write(out/'manifest.json',{'created_utc':datetime.now(timezone.utc).isoformat(),
        'git_head':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),
        'git_status':subprocess.check_output(['git','status','--short'],cwd=ROOT,text=True).strip(),
        'binaries':{key:{'path':str(binary),'sha256':hashes[key]} for key,binary in binaries.items()},
        'harness_sha256':sha(Path(__file__)),'imported_probe_sha256':sha(ROOT/'tools/compatibility/live_browser_comparison.py'),
        'user_script':str(USER_SCRIPT),'user_script_sha256':sha(USER_SCRIPT) if USER_SCRIPT.exists() else None,
        'preview_chrome_sha256':sha(CHROME) if args.preview!='off' else None,
        'arguments':{k:str(v) if isinstance(v,Path) else v for k,v in vars(args).items()},
        'notes':'One outstanding DOM probe. Timeout ends the entire trial and process. All contexts use user-script US locale. Retained mode preserves context cookies/cache across repetitions of one site; clean mode starts a fresh process. Preview enables -dev-preview and runs actual preview UI in separate pinned Chrome. Client CDP timings include transport and wait; server stage breakdown requires opt-in runtime diagnostics. Event tracing is separate from unprofiled timings. Live timings are content checks, not proof of full application equivalence.'})
    (out/'harness.py').write_bytes(Path(__file__).read_bytes())
    (out/'imported_probe.py').write_bytes((ROOT/'tools/compatibility/live_browser_comparison.py').read_bytes())
    results = []
    for mode in (['clean','retained'] if args.mode=='both' else [args.mode]):
        for preview_on in ([False,True] if args.preview=='both' else [args.preview=='on']):
            for index,site in enumerate(selected):
                retained = {}
                try:
                    for repetition in range(1,args.rounds+1):
                        order = list(binaries)
                        if (index+repetition)%2==0:
                            order.reverse()
                        for variant in order:
                            tag = f'{site[0]}-{variant}-{mode}-preview{int(preview_on)}-r{repetition}'
                            instance = retained.get(variant)
                            reused = instance is not None and instance.process.poll() is None
                            if not reused:
                                instance = Instance(binaries[variant],hashes[variant],out/tag,args)
                            result = await trial(instance,site,variant,repetition,mode,preview_on,out/tag,args,reused)
                            results.append(result)
                            if mode=='retained':
                                retained[variant] = instance
                            write(out/'summary.json',summarize(results))
                finally:
                    for instance in retained.values():
                        await instance.close()


if __name__=='__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--before',type=Path,required=True)
    parser.add_argument('--after',type=Path)
    parser.add_argument('--output',type=Path,required=True)
    parser.add_argument('--sites',default='chatgpt,github,spigotmc,modrinth,react,wikipedia')
    parser.add_argument('--rounds',type=int,default=5)
    parser.add_argument('--budget',type=float,default=60)
    parser.add_argument('--navigation-timeout',type=float,default=40)
    parser.add_argument('--mode',choices=('clean','retained','both'),default='clean')
    parser.add_argument('--preview',choices=('off','on','both'),default='off')
    parser.add_argument('--event-trace',action='store_true')
    parser.add_argument('--profile-cdp',action='store_true',help='Enable server timing and save Mimic.getTrace; diagnostic runs only')
    parser.add_argument('--env',action='append',default=[],help='Opt-in server diagnostics, NAME=VALUE; persisted in manifest')
    args = parser.parse_args()
    if args.rounds<1 or args.budget<=0 or args.navigation_timeout<=0 or any('=' not in pair for pair in args.env):
        parser.error('Positive rounds/timeouts and NAME=VALUE environment entries are required')
    asyncio.run(main(args))

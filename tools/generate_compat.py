#!/usr/bin/env python3
"""Generate Mimic's versioned Blink WebIDL and CDP surface catalogs.

The generator downloads only .idl files from the configured Chromium revision;
it never requires a Chromium checkout or runtime. Generated bindings expose
known shape and report semantic gaps through the browser host boundary.
"""

from __future__ import annotations

import argparse
import base64
import concurrent.futures
import hashlib
import io
import json
import re
import subprocess
import tarfile
import urllib.parse
import urllib.request
import urllib.error
import time
from collections import defaultdict
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
CACHE = ROOT / "compatibility" / ".source-cache"
GITILES = "https://chromium.googlesource.com/chromium/src/+/{ref}/{path}"
V8_GITILES = "https://chromium.googlesource.com/v8/v8/+/{ref}/{path}"


def write_utf8(path: Path, text: str) -> None:
    with path.open("w", encoding="utf-8", newline="\n") as stream:
        stream.write(text)


def fetch(url: str) -> bytes:
    request = urllib.request.Request(url, headers={"User-Agent": "Mimic compatibility generator"})
    for attempt in range(8):
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                return response.read()
        except urllib.error.HTTPError as error:
            if error.code != 429 or attempt == 7:
                raise
            time.sleep(min(30, 2 ** attempt))
    raise RuntimeError("unreachable")


def gitiles_json(ref: str, path: str) -> dict:
    CACHE.mkdir(parents=True, exist_ok=True)
    key = hashlib.sha256(f"{ref}:{path}".encode()).hexdigest()
    cached = CACHE / f"tree-{key}.json"
    if cached.exists():
        return json.loads(cached.read_text(encoding="utf-8"))
    raw = fetch(GITILES.format(ref=ref, path=path) + "?format=JSON")
    cached.write_bytes(raw[5:])
    time.sleep(0.12)
    return json.loads(raw[5:])


def gitiles_commit(ref: str) -> dict:
    raw = fetch(f"https://chromium.googlesource.com/chromium/src/+/{ref}?format=JSON")
    return json.loads(raw[5:])


def verify_target_lock(target: dict) -> dict:
    ref = target["chromium_ref"]
    commit = gitiles_commit(ref)
    if commit.get("commit") != target["chromium_commit"] or commit.get("tree") != target["chromium_tree"]:
        raise RuntimeError(f"Chromium ref drift: expected {target['chromium_commit']}/{target['chromium_tree']}, got {commit.get('commit')}/{commit.get('tree')}")
    tree_ids = {}
    for path, expected in target["blink_idl_roots"].items():
        actual = gitiles_json(ref, path).get("id")
        if actual != expected:
            raise RuntimeError(f"Blink tree drift for {path}: expected {expected}, got {actual}")
        tree_ids[path] = actual
    wpt_path = target["wpt_revision_source"]
    parent, name = wpt_path.rsplit("/", 1)
    entry = next((item for item in gitiles_json(ref, parent).get("entries", []) if item["name"] == name), None)
    if not entry or entry.get("id") != target["wpt_revision"]:
        raise RuntimeError(f"WPT revision drift: expected {target['wpt_revision']}, got {entry and entry.get('id')}")
    return {"chromiumCommit": commit["commit"], "chromiumTree": commit["tree"], "blinkTrees": tree_ids, "wptRevision": entry["id"]}


def list_idl_files(ref: str, roots: list[str]) -> list[str]:
    pending = list(roots)
    files: list[str] = []
    while pending:
        path = pending.pop()
        listing = gitiles_json(ref, path)
        for entry in listing.get("entries", []):
            child = f"{path}/{entry['name']}"
            if entry["type"] == "tree":
                pending.append(child)
            elif child.endswith(".idl"):
                files.append(child)
    return sorted(files)


def download_idls(ref: str, paths: list[str]) -> dict[str, str]:
    def one(path: str) -> tuple[str, str]:
        raw = fetch(GITILES.format(ref=ref, path=path) + "?format=TEXT")
        return path, base64.b64decode(raw).decode("utf-8")

    result: dict[str, str] = {}
    with concurrent.futures.ThreadPoolExecutor(max_workers=16) as pool:
        for path, text in pool.map(one, paths):
            result[path] = text
    return result


def download_idl_archives(ref: str, roots: list[str]) -> dict[str, str]:
    result: dict[str, str] = {}
    for root in roots:
        url = f"https://chromium.googlesource.com/chromium/src/+archive/{ref}/{root}.tar.gz"
        archive = fetch(url)
        with tarfile.open(fileobj=io.BytesIO(archive), mode="r:gz") as tar:
            for member in tar.getmembers():
                if not member.isfile() or not member.name.endswith(".idl"):
                    continue
                stream = tar.extractfile(member)
                if stream is not None:
                    result[f"{root}/{member.name}"] = stream.read().decode("utf-8")
    return result


def strip_comments(text: str) -> str:
    text = re.sub(r"/\*.*?\*/", "", text, flags=re.S)
    return re.sub(r"//[^\n]*", "", text)


def split_balanced(text: str, delimiter: str = ",") -> list[str]:
    out: list[str] = []
    start = 0
    depth = 0
    quote = ""
    for i, char in enumerate(text):
        if quote:
            if char == quote and (i == 0 or text[i - 1] != "\\"):
                quote = ""
            continue
        if char in "\"'":
            quote = char
        elif char in "([<{":
            depth += 1
        elif char in ")]>":
            depth = max(0, depth - 1)
        elif char == "}":
            depth = max(0, depth - 1)
        elif char == delimiter and depth == 0:
            out.append(text[start:i].strip())
            start = i + 1
    tail = text[start:].strip()
    if tail:
        out.append(tail)
    return out


def extended(raw: str | None) -> dict[str, str | bool | list[str] | dict[str, str]]:
    if not raw:
        return {}
    body = raw.strip()[1:-1]
    result: dict[str, str | bool | list[str] | dict[str, str]] = {}
    for item in split_balanced(body):
        conditional_exposure = re.fullmatch(r"Exposed\s*\((.*)\)", item, re.S)
        if conditional_exposure:
            entries = [entry.split() for entry in split_balanced(conditional_exposure.group(1))]
            result['Exposed'] = [entry[0] for entry in entries]
            result['ExposureFeatures'] = {entry[0]: entry[1] for entry in entries if len(entry) > 1}
            continue
        if "=" not in item:
            result[item.strip()] = True
            continue
        key, value = (part.strip() for part in item.split("=", 1))
        if value.startswith("(") and value.endswith(")"):
            result[key] = split_balanced(value[1:-1])
        else:
            result[key] = value
    return result


def find_braced_declarations(text: str):
    pattern = re.compile(
        r"(?P<ext>\[(?:[^\[\]]|\([^)]*\))*\]\s*)?"
        r"(?P<partial>partial\s+)?(?P<kind>callback\s+interface|interface\s+mixin|interface|dictionary|namespace|enum)\s+"
        r"(?P<name>[A-Za-z_][\w]*)(?:\s*:\s*(?P<parent>[A-Za-z_][\w]*))?\s*\{",
        re.S,
    )
    pos = 0
    while match := pattern.search(text, pos):
        depth = 1
        i = match.end()
        quote = ""
        while i < len(text) and depth:
            char = text[i]
            if quote:
                if char == quote and text[i - 1] != "\\":
                    quote = ""
            elif char in "\"'":
                quote = char
            elif char == "{":
                depth += 1
            elif char == "}":
                depth -= 1
            i += 1
        yield match, text[match.end() : i - 1]
        pos = i


def parse_arguments(raw: str) -> list[dict]:
    result = []
    for item in split_balanced(raw):
        ext_match = re.match(r"\s*(\[(?:[^\[\]]|\([^)]*\))*\])?\s*(.*)", item, re.S)
        arg_ext = extended(ext_match.group(1))
        body = ext_match.group(2).strip()
        default = None
        if "=" in body:
            body, default = (part.strip() for part in body.split("=", 1))
        optional = body.startswith("optional ")
        body = re.sub(r"^optional\s+", "", body)
        match = re.match(r"(.+?)\s+([A-Za-z_][\w]*)(\s*\.\.\.)?$", body)
        if match:
            result.append({"name": match.group(2), "type": match.group(1).strip(), "optional": optional, "variadic": bool(match.group(3)), "default": default, "extended": arg_ext})
    return result


def parse_members(body: str) -> list[dict]:
    members = []
    for raw in split_balanced(body, ";"):
        ext_match = re.match(r"\s*(\[(?:[^\[\]]|\([^)]*\))*\])?\s*(.*)", raw, re.S)
        member_ext = extended(ext_match.group(1))
        item = " ".join(ext_match.group(2).split())
        constructor = re.match(r"constructor\s*\((.*)\)$", item)
        if constructor:
            members.append({"kind": "constructor", "arguments": parse_arguments(constructor.group(1)), "extended": member_ext})
            continue
        attribute = re.match(r"(?:(static)\s+)?(?:(readonly)\s+)?attribute\s+(.+?)\s+([A-Za-z_][\w]*)$", item)
        if attribute:
            members.append({"kind": "attribute", "name": attribute.group(4), "type": attribute.group(3), "static": bool(attribute.group(1)), "readonly": bool(attribute.group(2)), "extended": member_ext})
            continue
        constant = re.match(r"const\s+(.+?)\s+([A-Za-z_][\w]*)\s*=\s*(.+)$", item)
        if constant:
            members.append({"kind": "constant", "name": constant.group(2), "type": constant.group(1), "value": constant.group(3), "extended": member_ext})
            continue
        operation = re.match(r"(?:(static|stringifier|getter|setter|deleter)\s+)*(.*?)\s+([A-Za-z_][\w]*)\s*\((.*)\)$", item)
        if operation:
            qualifiers = item[: operation.start(2)].split()
            members.append({"kind": "operation", "name": operation.group(3), "returnType": operation.group(2), "arguments": parse_arguments(operation.group(4)), "static": "static" in qualifiers, "special": [q for q in qualifiers if q != "static"], "extended": member_ext})
    return members


def normalize_idl(sources: dict[str, str], target: dict) -> dict:
    declarations: list[dict] = []
    includes: list[dict] = []
    callbacks: list[dict] = []
    typedefs: list[dict] = []
    for path, source in sources.items():
        text = strip_comments(source)
        for match, body in find_braced_declarations(text):
            kind = " ".join(match.group("kind").split())
            item = {"kind": kind, "name": match.group("name"), "parent": match.group("parent"), "partial": bool(match.group("partial")), "extended": extended(match.group("ext")), "source": path}
            if kind == "enum":
                item["values"] = re.findall(r'"([^"]*)"', body)
            else:
                item["members"] = parse_members(body)
                for member in item["members"]:
                    member["origin"] = {"interface": item["name"], "source": path,
                                        "extended": dict(item["extended"])}
            declarations.append(item)
        includes.extend({"target": a, "mixin": b, "source": path} for a, b in re.findall(r"\b([A-Za-z_]\w*)\s+includes\s+([A-Za-z_]\w*)\s*;", text))
        for match in re.finditer(r"(?:\[[^;]*?\]\s*)?callback\s+([A-Za-z_]\w*)\s*=\s*(.+?)\s*\((.*?)\)\s*;", text, re.S):
            callbacks.append({"name": match.group(1), "returnType": " ".join(match.group(2).split()), "arguments": parse_arguments(match.group(3)), "source": path})
        for match in re.finditer(r"\btypedef\s+(.+?)\s+([A-Za-z_]\w*)\s*;", text):
            typedefs.append({"name": match.group(2), "type": " ".join(match.group(1).split()), "source": path})

    primary: dict[tuple[str, str], dict] = {}
    partials: list[dict] = []
    for decl in declarations:
        if decl["partial"]:
            partials.append(decl)
        else:
            primary[(decl["kind"], decl["name"])] = decl
    for partial in partials:
        candidates = [key for key in primary if key[1] == partial["name"]]
        if candidates:
            base = primary[candidates[0]]
            base.setdefault("partials", []).append(partial["source"])
            base.setdefault("members", []).extend(partial.get("members", []))
            # Partial-interface conditions belong to its members, never to the
            # primary interface (e.g. Navigator must not become WebShare-only).
        else:
            primary[(partial["kind"], partial["name"])] = partial
    mixins = {name: decl for (kind, name), decl in primary.items() if kind == "interface mixin"}
    by_name = {name: decl for (kind, name), decl in primary.items() if kind in {"interface", "callback interface"}}
    for relation in includes:
        target_decl, mixin = by_name.get(relation["target"]), mixins.get(relation["mixin"])
        if target_decl and mixin:
            target_decl.setdefault("includes", []).append(relation["mixin"])
            target_decl.setdefault("members", []).extend(mixin.get("members", []))
    return {
        "schemaVersion": 1,
        "chromeVersion": target["chrome_version"],
        "chromiumRef": target["chromium_ref"],
        "sourceFileCount": len(sources),
        "declarations": sorted(primary.values(), key=lambda x: (x["kind"], x["name"])),
        "includes": includes,
        "callbacks": callbacks,
        "typedefs": typedefs,
    }


def parse_js_pdl(text: str) -> list[dict]:
    domains: list[dict] = []
    current = None
    item = None
    section = None
    for line in text.splitlines():
        stripped = line.strip()
        domain = re.match(r"domain\s+(\w+)", stripped)
        if domain:
            current = {"domain": domain.group(1), "commands": [], "events": [], "types": []}
            domains.append(current)
            item = None
            continue
        if current is None:
            continue
        match = re.match(r"command\s+(\w+)", stripped)
        if match:
            item = {"name": match.group(1)}
            current["commands"].append(item)
            section = None
            continue
        match = re.match(r"event\s+(\w+)", stripped)
        if match:
            item = {"name": match.group(1)}
            current["events"].append(item)
            section = None
            continue
        if stripped in {"parameters", "returns"}:
            section = stripped
            if item is not None:
                item.setdefault(section, [])
            continue
        if item is not None and section and line.startswith("      "):
            field = re.match(r"(?:optional\s+)?(?:array of\s+)?(?:\$ref\s+)?([\w.]+)\s+(\w+)", stripped)
            if field:
                item[section].append({"name": field.group(2), "type": field.group(1), "optional": stripped.startswith("optional ")})
    return domains


def generate_protocol(target: dict) -> dict:
    browser_raw = fetch(GITILES.format(ref=target["chromium_ref"], path=target["browser_protocol_json"]) + "?format=TEXT")
    browser = json.loads(base64.b64decode(browser_raw))
    js_raw = fetch(V8_GITILES.format(ref=target["v8_revision"], path="include/js_protocol.pdl") + "?format=TEXT")
    js_domains = parse_js_pdl(base64.b64decode(js_raw).decode("utf-8"))
    domains = browser["domains"] + js_domains
    return {
        "schemaVersion": 1,
        "chromeVersion": target["chrome_version"],
        "chromiumRef": target["chromium_ref"],
        "chromiumCommit": target["chromium_commit"],
        "v8Revision": target["v8_revision"],
        "browserProtocolSHA256": hashlib.sha256(base64.b64decode(browser_raw)).hexdigest(),
        "jsProtocolSHA256": hashlib.sha256(base64.b64decode(js_raw)).hexdigest(),
        "domains": domains,
    }


def js_string(value) -> str:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))


def surface_catalog(catalog: dict) -> str:
    interfaces = []
    for decl in catalog["declarations"]:
        if decl["kind"] not in {"interface", "namespace"}:
            continue
        attributes = dict(decl.get("extended", {}))
        # Retained IDL predates the conditional-Exposed parser. Normalize its
        # spelling in every projection, not just capability feature gating.
        for key in list(attributes):
            if key.startswith('Exposed('):
                attributes.update(extended('[' + key + ']'))
        exposed = attributes.get("Exposed", [])
        if isinstance(exposed, str):
            exposed = [exposed]
        if exposed is True:
            exposed = ["Window"]
        aliases = decl.get("extended", {}).get("LegacyWindowAlias", [])
        if isinstance(aliases, str):
            aliases = [aliases]
        members = []
        for member in decl.get("members", []):
            if member["kind"] in {"attribute", "operation", "constant"} and member.get("name"):
                members.append({key: member.get(key) for key in ("kind", "name", "readonly", "static", "value") if key in member})
        interfaces.append({**({"kind":"namespace"} if decl["kind"] == "namespace" else {}), "name": decl["name"], "parent": decl.get("parent"), "exposed": exposed, "legacyWindowAliases": aliases, "constructible": any(m["kind"] == "constructor" for m in decl.get("members", [])), "members": members})
    return js_string(interfaces)


def generate_surface_js(catalog: dict) -> str:
    navigator = next(d for d in catalog['declarations'] if d['name'] == 'Navigator')
    navigator_members = js_string([m for m in navigator.get('members', []) if m['kind'] in {'attribute', 'operation'}])
    permission_names = js_string(next(d['values'] for d in catalog['declarations'] if d['name'] == 'PermissionName'))
    capability_types = {m.get('type') for m in navigator.get('members', [])}
    capability_types.update(['PermissionStatus', 'StorageBucket', 'Lock', 'MediaDeviceInfo', 'DelegatedInkTrailPresenter', 'KeyboardLayoutMap'])
    capability_interfaces = js_string({d['name']:d.get('members',[]) for d in catalog['declarations'] if d['name'] in capability_types})
    conditions = {}
    for declaration in catalog['declarations']:
        if declaration['kind'] != 'interface': continue
        attributes = dict(declaration.get('extended', {}))
        # Older retained declarations can still contain Blink's conditional
        # spelling as a key. Interpret it without changing its recorded source.
        for key in list(attributes):
            if key.startswith('Exposed('): attributes.update(extended('[' + key + ']'))
        conditions[declaration['name']] = {key:attributes[key] for key in ['RuntimeEnabled','ExposureFeatures'] if key in attributes}
    capability_conditions = js_string(conditions)
    # Literal names give each constructor its own engine function metadata.
    # A computed property name changes the public .name but leaves V8's shared
    # constructor record anonymous; renaming that record would affect siblings.
    constructor_names = sorted(d['name'] for d in catalog['declarations'] if d['kind'] == 'interface')
    for name in constructor_names:
        if not re.fullmatch(r"[A-Za-z_$][A-Za-z0-9_$]*", name):
            raise ValueError(f"Invalid interface identifier: {name}")
    constructor_cases = "\n".join(
        f"      case {js_string(name)}: return function {name}(...args) {{ return invoke(this, args, new.target); }};"
        for name in constructor_names
    )
    return f"""// Code generated by tools/generate_compat.py for Chrome {catalog['chromeVersion']}; DO NOT EDIT.
(function(host){{
  let catalog=JSON.parse(host.catalogJSON());
  const globals=globalThis;
  const lazy=globals.__mimicLazySurface;
  // V8's native factory keeps one dispatcher per realm and gives every
  // generated interface its own named, constructible native function. Goja
  // retains the original JS factory path and its exact function semantics.
  const nativeConstructorFactory=typeof host.createGeneratedConstructorFactory==='function'
    ? host.createGeneratedConstructorFactory : null;
  const nativeConstructorNames=[];
  const dispatchGeneratedConstructor=(id,receiver,args,newTarget)=>{{
    const interfaceName=nativeConstructorNames[id];
    if(typeof interfaceName!=='string')throw new RangeError('Unknown generated interface');
    const domain=lazy&&lazy.domainFor(interfaceName);
    if(domain){{lazy.cell(interfaceName,'constructor');return lazy.invoke(interfaceName,'constructor',receiver,args,newTarget)}}
    if(interfaceName.startsWith('HTML')&&typeof constructCustomElement==='function')return constructCustomElement(newTarget);
    host.semanticMissing(interfaceName+'.constructor');
    throw new TypeError('Illegal constructor');
  }};
  if(nativeConstructorFactory){{
    nativeConstructorFactory(dispatchGeneratedConstructor);
    bootstrapRestoreHooks.push(()=>host.createGeneratedConstructorFactory(dispatchGeneratedConstructor));
  }}
  const makeJSConstructor=(name,invoke)=>{{
    switch(name){{
{constructor_cases}
      default: throw new RangeError('Unknown generated interface');
    }}
  }};
  if(String(globalThis.__mimicIDLExposure||'Window')==='Window')globals.__mimicNavigatorMembers={navigator_members};
  if(String(globalThis.__mimicIDLExposure||'Window')==='Window')globals.__mimicPermissionNames={permission_names};
  if(String(globalThis.__mimicIDLExposure||'Window')==='Window')globals.__mimicCapabilityInterfaces={capability_interfaces};
  if(String(globalThis.__mimicIDLExposure||'Window')==='Window')globals.__mimicCapabilityConditions={capability_conditions};
  const realmExposure=String(globalThis.__mimicIDLExposure||'Window');
  const attributeUnsafe=globalThis.__mimicAttributeUnsafeInterfaces||new Set();
  const exposed=spec=>spec.exposed.includes('*')||spec.exposed.includes(realmExposure)||(realmExposure==='DedicatedWorker'&&spec.exposed.includes('Worker'));
  const missing=(iface,member)=>host.semanticMissing(iface+'.'+member);
  for(const spec of catalog){{
    if(!exposed(spec))continue;
    if(spec.kind==='namespace'){{
      let namespace=globals[spec.name];
      if(!namespace||typeof namespace!=='object'){{namespace={{}};Object.defineProperty(globals,spec.name,{{value:namespace,writable:true,configurable:true}})}}
      if(!Object.hasOwn(namespace,Symbol.toStringTag))Object.defineProperty(namespace,Symbol.toStringTag,{{value:spec.name,configurable:true}});
      for(const member of spec.members){{
        if(member.name in namespace)continue;
        if(member.kind==='constant')Object.defineProperty(namespace,member.name,{{value:Number(member.value),enumerable:true}});
        else if(member.kind==='operation'){{
          const interfaceName=spec.name,memberName=member.name;
          Object.defineProperty(namespace,memberName,{{value:function(){{return missing(interfaceName,memberName)}},writable:true,enumerable:true,configurable:true}});
        }}
      }}
      continue;
    }}
    const interfaceName=spec.name;
    const lazyDomain=lazy&&lazy.domainFor(interfaceName);
    let ctor=globals[spec.name];
    const generated=typeof ctor!=='function';
    if(generated){{
      if(nativeConstructorFactory&&!lazyDomain){{
        const id=nativeConstructorNames.length;
        nativeConstructorNames.push(interfaceName);
        ctor=nativeConstructorFactory(interfaceName,id);
      }} else ctor=makeJSConstructor(interfaceName,(receiver,args,newTarget)=>{{
        if(lazyDomain){{lazy.cell(interfaceName,'constructor');return lazy.invoke(interfaceName,'constructor',receiver,args,newTarget)}}
        if(interfaceName.startsWith('HTML')&&typeof constructCustomElement==='function')return constructCustomElement(newTarget);
        missing(interfaceName,'constructor');throw new TypeError('Illegal constructor');
      }});
      Object.defineProperty(globals,spec.name,{{value:ctor,writable:true,configurable:true}});
    }}
    if(lazyDomain)lazy.cell(interfaceName,'constructor');
    if(!Object.hasOwn(ctor.prototype,Symbol.toStringTag))Object.defineProperty(ctor.prototype,Symbol.toStringTag,{{value:spec.name,configurable:true}});
    if(realmExposure==='Window')for(const alias of spec.legacyWindowAliases||[])Object.defineProperty(globals,alias,{{value:ctor,writable:true,configurable:true}});
    const parent=spec.parent&&globals[spec.parent];
    if(parent&&parent.prototype&&Object.getPrototypeOf(ctor.prototype)!==parent.prototype)Object.setPrototypeOf(ctor.prototype,parent.prototype);
    // Handwritten members own semantics; IDL-generated fallbacks complete the
    // remaining pinned surface without overwriting those implementations.
    for(const member of spec.members){{
      const memberName=member.name;
      // Interface constants are own immutable properties of both the interface
      // object and its prototype, including handwritten interface objects.
      if(member.kind==='constant'){{
        for(const object of [ctor,ctor.prototype])if(!Object.hasOwn(object,member.name))Object.defineProperty(object,member.name,{{value:Number(member.value)||0,enumerable:true}});
        continue;
      }}
      const target=member.static?ctor:ctor.prototype;
      if(member.name in target){{
        // JavaScript class syntax makes methods/accessors non-enumerable, while
        // WebIDL prototype members in Blink are enumerable. Normalize existing
        // handwritten semantics from the pinned IDL instead of maintaining a
        // second manual descriptor table.
        const descriptor=Object.getOwnPropertyDescriptor(target,member.name);
        if(descriptor&&descriptor.configurable&&!descriptor.enumerable)Object.defineProperty(target,member.name,Object.assign({{}},descriptor,{{enumerable:true}}));
        continue;
      }}
      // Only explicitly declared legacy implementations still use observable
      // own-field assignment as internal storage. Slot-backed handwritten
      // interfaces receive the complete generated IDL surface too.
      if(!generated&&member.kind==='attribute'&&attributeUnsafe.has(spec.name))continue;
      if(member.kind==='operation'){{
        // A generated fallback has no JS algorithm to run at bootstrap. Keep
        // its own-key position while native V8 publishes its value on demand.
        if(!member.static&&(generated||lazyDomain)&&typeof stageGeneratedLazyOperation==='function'&&stageGeneratedLazyOperation(target,interfaceName,memberName,lazyDomain)){{
          if(lazyDomain)lazy.cell(interfaceName,memberName,'value');
          continue;
        }}
        const value=lazyDomain?{{[memberName]:function(...args){{return lazy.invoke(interfaceName,memberName,this,args,undefined,'value')}}}}[memberName]:function(){{return missing(interfaceName,memberName)}};
        if(lazyDomain)lazy.cell(interfaceName,memberName,'value');
        Object.defineProperty(target,member.name,{{value,writable:true,enumerable:true,configurable:true}});
      }}
      else {{
        const accessors=lazyDomain?{{get [memberName](){{return lazy.invoke(interfaceName,memberName,this,[],undefined,'get')}},set [memberName](value){{return lazy.invoke(interfaceName,memberName,this,[value],undefined,'set')}}}}:null;
        const get=accessors?Object.getOwnPropertyDescriptor(accessors,memberName).get:function(){{return missing(interfaceName,memberName)}};
        const set=member.readonly?undefined:(accessors?Object.getOwnPropertyDescriptor(accessors,memberName).set:function(){{return missing(interfaceName,memberName)}});
        if(lazyDomain){{lazy.cell(interfaceName,memberName,'get');if(set)lazy.cell(interfaceName,memberName,'set')}}
        Object.defineProperty(target,member.name,{{get,set,enumerable:true,configurable:true}});
      }}
    }}
  }}
  // Interface declarations are sorted for reproducible output, not in base
  // class order. Link inheritance only after every exposed constructor exists;
  // otherwise an alphabetically earlier derived interface silently loses its
  // WebIDL prototype chain (for example DedicatedWorkerGlobalScope).
  for(const spec of catalog){{
    if(!exposed(spec)||!spec.parent)continue;
    const ctor=globals[spec.name],parent=globals[spec.parent];
    if(ctor&&ctor.prototype&&parent&&parent.prototype&&Object.getPrototypeOf(ctor.prototype)!==parent.prototype)Object.setPrototypeOf(ctor.prototype,parent.prototype);
    // Interface objects inherit too: static members and reflection must use
    // the same realm's parent, including parents declared later in the catalog.
    if(typeof ctor==='function'&&typeof parent==='function'&&Object.getPrototypeOf(ctor)!==parent)Object.setPrototypeOf(ctor,parent);
  }}
  // No generated function requires the parsed declaration graph after shape
  // publication. Clear the sole root so every realm retains only descriptors
  // and the primitive dispatch metadata captured by its facades.
  catalog=null;
}})(__mimic);
"""


def generate_protocol_go(protocol: dict) -> str:
    methods = []
    events = []
    for domain in protocol["domains"]:
        name = domain.get("domain")
        for command in domain.get("commands", []):
            methods.append(f"{name}.{command['name']}")
        for event in domain.get("events", []):
            events.append(f"{name}.{event['name']}")
    method_lines = "\n".join(f'\t"{name}": {{}},' for name in sorted(set(methods)))
    event_lines = "\n".join(f'\t"{name}": {{}},' for name in sorted(set(events)))
    return f'''// Code generated by tools/generate_compat.py for Chrome {protocol["chromeVersion"]}; DO NOT EDIT.
package generated

import _ "embed"

//go:embed surface.js
var Surface string

//go:embed surface-catalog.json
var SurfaceCatalog string

var protocolMethods = map[string]struct{{}}{{
{method_lines}
}}

var protocolEvents = map[string]struct{{}}{{
{event_lines}
}}

func ProtocolMethods() map[string]struct{{}} {{ return clone(protocolMethods) }}
func ProtocolEvents() map[string]struct{{}} {{ return clone(protocolEvents) }}

func clone(source map[string]struct{{}}) map[string]struct{{}} {{
	result := make(map[string]struct{{}}, len(source))
	for name := range source {{ result[name] = struct{{}}{{}} }}
	return result
}}
'''


def check_generated(target: dict) -> None:
    """Check retained inputs and deterministic projections without any network I/O."""
    output = ROOT / target['generated_dir']
    lock = json.loads((output / 'lock.json').read_text(encoding='utf-8'))
    catalog = json.loads((output / 'webapi.json').read_text(encoding='utf-8'))
    protocol = json.loads((output / 'cdp.json').read_text(encoding='utf-8'))
    for source, key in [('chrome_version', 'chromeVersion'), ('chromium_commit', 'chromiumCommit'),
                        ('chromium_ref', 'chromiumRef'), ('v8_revision', 'v8Revision')]:
        if lock[key] != target[source] or protocol[key] != target[source]:
            raise RuntimeError(f'Pinned metadata mismatch: {key}')
    for source, key in [('main_branch_revision', 'mainBranchRevision'), ('main_branch_commit', 'mainBranchCommit'), ('differential_browser', 'differentialBrowser')]:
        if lock[key] != target[source]:
            raise RuntimeError(f'Pinned target mismatch: {key}')
    if lock['blinkTrees'] != target['blink_idl_roots'] or lock['wptRevision'] != target['wpt_revision']:
        raise RuntimeError('Blink/WPT metadata mismatch')
    if hashlib.sha256((output / 'webapi.json').read_bytes()).hexdigest() != lock['webIDLDatabaseSHA256']:
        raise RuntimeError('WebIDL catalog hash mismatch')
    navigator_sources = output / 'navigator-idl-sources.json'
    if navigator_sources.exists():
        retained = json.loads(navigator_sources.read_text(encoding='utf-8'))
        if retained['chromiumCommit'] != target['chromium_commit']:
            raise RuntimeError('Navigator source revision mismatch')
        recovered = normalize_idl(retained['sources'], target)
        declarations = {d['name']:d for d in catalog['declarations']}
        for declaration in recovered['declarations']:
            if declarations.get(declaration['name']) != declaration:
                raise RuntimeError(f"Non-reproducible Navigator provenance: {declaration['name']}")
    for key in ['browserProtocolSHA256', 'jsProtocolSHA256']:
        if protocol[key] != lock[key]:
            raise RuntimeError(f'Protocol provenance mismatch: {key}')
    expected = {'surface.js': generate_surface_js(catalog).encode('utf-8'),
                'surface-catalog.json': surface_catalog(catalog).encode('utf-8'),
                'bundle_data.go': subprocess.run(['gofmt'], input=generate_protocol_go(protocol).encode('utf-8'),
                                                 capture_output=True, check=True).stdout}
    for name, content in expected.items():
        if (output / name).read_bytes() != content:
            raise RuntimeError(f'Non-reproducible projection: {name}')
    hashes = json.loads((output / 'artifact-hashes.json').read_text(encoding='utf-8'))
    actual_names = {p.name for p in output.iterdir() if p.is_file() and p.name != 'artifact-hashes.json'}
    if set(hashes) != actual_names:
        raise RuntimeError('Artifact manifest does not cover exactly the retained files')
    for name, digest in hashes.items():
        if hashlib.sha256((output / name).read_bytes()).hexdigest() != digest:
            raise RuntimeError(f'Artifact hash mismatch: {name}')
    print('PASS: pinned metadata, artifact hashes, deterministic WebIDL/CDP projections (offline)')
    print('Raw upstream IDL/CDP download and Chrome exposure recapture are separate checks; not performed.')
    # The legacy stable-1.3 input above stays frozen. The runtime wire schema
    # comes from the complete exact-Chrome protocol retained independently.
    from generate_cdp import generate as generate_cdp
    generate_cdp(ROOT, check=True)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--target", default="chrome/152/target.json")
    parser.add_argument("--skip-idl-download", action="store_true")
    parser.add_argument("--check", action="store_true", help="verify retained artifacts offline; never downloads")
    args = parser.parse_args()
    target_path = (ROOT / args.target).resolve()
    target = json.loads(target_path.read_text(encoding="utf-8"))
    if args.check:
        check_generated(target)
        return
    lock = verify_target_lock(target)
    output = ROOT / target.get("generated_dir", f"chrome/{target['milestone']}/generated")
    output.mkdir(parents=True, exist_ok=True)
    idl_json = output / "webapi.json"
    if args.skip_idl_download and idl_json.exists():
        catalog = json.loads(idl_json.read_text(encoding="utf-8"))
    else:
        roots = list(target["blink_idl_roots"])
        print(f"Downloading pinned Blink IDL archives from {target['chromium_ref']}...", flush=True)
        sources = download_idl_archives(target["chromium_ref"], roots)
        print(f"Normalizing {len(sources)} Blink IDL files...", flush=True)
        catalog = normalize_idl(sources, target)
    write_utf8(idl_json, json.dumps(catalog, ensure_ascii=False, indent=2) + "\n")
    protocol = generate_protocol(target)
    write_utf8(output / "cdp.json", json.dumps(protocol, ensure_ascii=False, indent=2) + "\n")
    write_utf8(output / "surface.js", generate_surface_js(catalog))
    write_utf8(output / "surface-catalog.json", surface_catalog(catalog))
    generated_go = output / "bundle_data.go"
    write_utf8(generated_go, generate_protocol_go(protocol))
    subprocess.run(["gofmt", "-w", str(generated_go)], check=True)
    lock.update({
        "schemaVersion": 1,
        "chromeVersion": target["chrome_version"],
        "chromiumRef": target["chromium_ref"],
        "v8Revision": target["v8_revision"],
        "mainBranchRevision": target.get("main_branch_revision"),
        "mainBranchCommit": target.get("main_branch_commit"),
        "browserProtocolSHA256": protocol["browserProtocolSHA256"],
        "jsProtocolSHA256": protocol["jsProtocolSHA256"],
        "webIDLDatabaseSHA256": hashlib.sha256(idl_json.read_bytes()).hexdigest(),
        "differentialBrowser": target["differential_browser"],
    })
    write_utf8(output / "lock.json", json.dumps(lock, indent=2) + "\n")
    print(f"Generated {len(catalog['declarations'])} IDL declarations and {len(protocol['domains'])} CDP domains", flush=True)


if __name__ == "__main__":
    main()

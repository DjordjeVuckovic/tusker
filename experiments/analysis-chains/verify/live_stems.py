"""Independent live check of stemmer outputs on fresh containers (not the experiment pipeline)."""
import json, subprocess, sys, urllib.request

WORDS = ("added adding anning ebbed ebbing egged epping erred erring offing "
         "university evening organization organizations emergency news studies universe running "
         "ads ad add better was is as us technology skies https generously").split()

def pg(container, sql):
    r = subprocess.run(["docker","exec",container,"psql","-U","postgres","-tAF","\t","-c",sql],capture_output=True,text=True)
    if r.returncode: raise SystemExit(r.stderr)
    return r.stdout

def pg_lexize(container, dictionary):
    arr = ",".join("'%s'" % w for w in WORDS)
    out = pg(container, f"select w, coalesce(array_to_string(ts_lexize('{dictionary}', w),'|'),'<null>') from unnest(array[{arr}]) w")
    return dict(l.split("\t") for l in out.strip().splitlines())

def pdb_stem():
    arr = ",".join("'%s'" % w for w in WORDS)
    out = pg("vf-pdb", f"select w, array_to_string(w::pdb.simple('stemmer=english')::text[],'|') from unnest(array[{arr}]) w")
    return dict(l.split("\t") for l in out.strip().splitlines())

def es(filters):
    res = {}
    for w in WORDS:
        body = json.dumps({"tokenizer":"keyword","filter":filters,"text":w}).encode()
        req = urllib.request.Request("http://localhost:55804/_analyze", body, {"content-type":"application/json"})
        toks = json.load(urllib.request.urlopen(req))["tokens"]
        res[w] = "|".join(t["token"] for t in toks)
    return res

out = {
  "pg18.1 english_stem": pg_lexize("vf-pg","english_stem"),
  "pg18.4 english_stem": pg_lexize("vf-tiger","english_stem"),
  "pdb stemmer=english": pdb_stem(),
  "es snowball English": es([{"type":"snowball","language":"English"}]),
  "es stemmer porter2": es([{"type":"stemmer","language":"porter2"}]),
  "es stemmer english": es([{"type":"stemmer","language":"english"}]),
  "es porter_stem": es(["porter_stem"]),
  "es kstem": es(["kstem"]),
  "es stemmer light_english": es([{"type":"stemmer","language":"light_english"}]),
  "es stemmer minimal_english": es([{"type":"stemmer","language":"minimal_english"}]),
  "es stemmer lovins": es([{"type":"stemmer","language":"lovins"}]),
}
json.dump(out, open("live_stems.json","w"), indent=1)

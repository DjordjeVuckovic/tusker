import json, subprocess, urllib.request
V = sorted(json.load(open('/Users/djordjevuckovic/projects/news-hunter/experiments/analysis-chains/out/vocab.json')))
def psql(c, sql, stdin=None):
    r = subprocess.run(["docker","exec","-i",c,"psql","-U","postgres","-v","ON_ERROR_STOP=1","-tAF","\t","-c",sql],input=stdin,capture_output=True,text=True)
    if r.returncode: raise SystemExit(c+r.stderr)
    return r.stdout
def pg_full(c):
    psql(c, "drop table if exists vw; create table vw(w text)")
    r = subprocess.run(["docker","exec","-i",c,"psql","-U","postgres","-c","copy vw from stdin"],input="\n".join(V)+"\n",capture_output=True,text=True); assert r.returncode==0, r.stderr
    psql(c, "drop text search dictionary if exists vf_stem_nostop; create text search dictionary vf_stem_nostop (template=snowball, language=english)")
    out = psql(c, "select w, array_to_string(ts_lexize('vf_stem_nostop', w),'|') from vw")
    d = dict(l.split("\t") for l in out.splitlines() if l)
    assert len(d)==len(V); return d
def pdb_full():
    c="vf-pdb"; psql(c,"drop table if exists vw; create table vw(w text)")
    r = subprocess.run(["docker","exec","-i",c,"psql","-U","postgres","-c","copy vw from stdin"],input="\n".join(V)+"\n",capture_output=True,text=True); assert r.returncode==0
    out = psql(c, "select w, array_to_string(w::pdb.simple('stemmer=english')::text[],'|') from vw")
    d = dict(l.split("\t") for l in out.splitlines() if l); assert len(d)==len(V); return d
def es_full(filters):
    res = {}
    for i in range(0, len(V), 2000):
        chunk = V[i:i+2000]
        body = json.dumps({"tokenizer":"keyword","filter":filters,"text":chunk}).encode()
        toks = json.load(urllib.request.urlopen(urllib.request.Request("http://localhost:55804/_analyze", body, {"content-type":"application/json"})))["tokens"]
        by = {}
        for t in toks:
            idx, rem = divmod(t["position"], 101); assert rem == 0, t
            by.setdefault(idx, []).append(t["token"])
        for j, w in enumerate(chunk): res[w] = "|".join(by.get(j, []))
    return res
out = {"pg18.1": pg_full("vf-pg"), "pg18.4": pg_full("vf-tiger"), "pdb": pdb_full(),
       "es.snowball": es_full([{"type":"snowball","language":"English"}]),
       "es.porter2": es_full([{"type":"stemmer","language":"porter2"}]),
       "es.english": es_full([{"type":"stemmer","language":"english"}]),
       "es.porter_stem": es_full(["porter_stem"]),
       "es.kstem": es_full(["kstem"]),
       "es.light_english": es_full([{"type":"stemmer","language":"light_english"}])}
json.dump(out, open("full_live.json","w"))
print({k: len(v) for k,v in out.items()})

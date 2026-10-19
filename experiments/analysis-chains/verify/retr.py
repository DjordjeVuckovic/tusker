import json, subprocess, urllib.request
A='/Users/djordjevuckovic/projects/news-hunter/experiments/analysis-chains/out/'
S=[json.loads(l) for l in open(A+'sample_b.jsonl')]
Q=json.load(open(A+'queries.json'))['queries']
STORED=json.load(open(A+'divergence_results.json'))['results']
assert Q==json.load(open(A+'divergence_results.json'))['queries']
def es(method,path,body=None,nd=False):
    data=None if body is None else (body if nd else json.dumps(body)).encode()
    req=urllib.request.Request("http://localhost:55804"+path,data,{"content-type":"application/x-ndjson" if nd else "application/json"},method=method)
    return json.load(urllib.request.urlopen(req))
F=("title","description","content")
sub={"p2":["lowercase","lstop","p2"],"mn":["lowercase","lstop","mn"]}
mp={f:{"type":"text","fields":{k:{"type":"text","analyzer":"a_"+k} for k in sub}} for f in F}; mp["line"]={"type":"integer"}
try: es("DELETE","/vfr")
except Exception: pass
es("PUT","/vfr",{"settings":{"number_of_shards":1,"number_of_replicas":0,"analysis":{"filter":{"lstop":{"type":"stop","stopwords":"_english_"},"p2":{"type":"stemmer","language":"porter2"},"mn":{"type":"stemmer","language":"minimal_english"}},"analyzer":{"a_"+k:{"tokenizer":"standard","filter":v} for k,v in sub.items()}}},"mappings":{"properties":mp}})
for i in range(0,len(S),2000):
    nd="".join(json.dumps({"index":{"_index":"vfr","_id":str(r["line"])}})+"\n"+json.dumps({"line":r["line"],**{f:r[f] for f in F}})+"\n" for r in S[i:i+2000])
    assert not es("POST","/_bulk",nd,nd=True)["errors"]
es("POST","/vfr/_refresh"); es("POST","/vfr/_forcemerge?max_num_segments=1")
def es_s(k,q):
    b={"size":10,"_source":False,"query":{"multi_match":{"query":q,"type":"cross_fields","operator":"and","fields":[f"{f}.{k}" for f in F]}},"sort":[{"_score":"desc"},{"line":"asc"}]}
    return [int(h["_id"]) for h in es("POST","/vfr/_search?request_cache=false",b)["hits"]["hits"]]
res={"es.standard+lucenestop+porter2":[es_s("p2",q) for q in Q],"es.standard+lucenestop+minimal":[es_s("mn",q) for q in Q]}
# PG
def psql(sql,stdin=None):
    r=subprocess.run(["docker","exec","-i","vf-pg","psql","-U","postgres","-v","ON_ERROR_STOP=1","-tA","-c",sql],input=stdin,capture_output=True,text=True)
    if r.returncode: raise SystemExit(r.stderr)
    return r.stdout
psql("drop table if exists rb; create table rb(line int primary key, title text, description text, content text)")
import csv,io
buf=io.StringIO(); w=csv.writer(buf)
for r in S: w.writerow([r["line"],r["title"],r["description"],r["content"]])
r=subprocess.run(["docker","exec","-i","vf-pg","psql","-U","postgres","-c","copy rb from stdin with (format csv, force_not_null (title,description,content))"],input=buf.getvalue(),capture_output=True,text=True); assert r.returncode==0,r.stderr
for cfg in ("english","simple"):
    psql(f"alter table rb add column tsv_{cfg} tsvector; update rb set tsv_{cfg}=setweight(to_tsvector('{cfg}',title),'A')||setweight(to_tsvector('{cfg}',description),'B')||setweight(to_tsvector('{cfg}',content),'C')")
qs=json.dumps(Q)
for cfg,name in (("english","pg.english"),("simple","pg.simple")):
    out=psql(f"""select i, coalesce((select string_agg(line::text,',' order by rk desc, line asc) from (select line, ts_rank(tsv_{cfg},q) rk from rb, plainto_tsquery('{cfg}', x) q where tsv_{cfg} @@ q order by rk desc, line asc limit 10) s),'') from json_array_elements_text('{qs.replace("'","''")}'::json) with ordinality as t(x,i)""")
    rows=[l.split('|') for l in out.strip().splitlines()]
    res[name]=[[int(v) for v in r[1].split(',')] if r[1] else [] for r in sorted(rows,key=lambda r:int(r[0]))]
for k,v in res.items():
    same=sum(1 for a,b in zip(v,STORED[k]) if a==b)
    print(k, f"{same}/{len(Q)} identical ordered lists")
    bad=[(Q[i],a,b) for i,(a,b) in enumerate(zip(v,STORED[k])) if a!=b][:2]
    if bad: print("  e.g.",bad)

import json, subprocess, urllib.request
V = sorted(json.load(open('/Users/djordjevuckovic/projects/news-hunter/experiments/analysis-chains/out/vocab.json')))
def psql(sql):
    r=subprocess.run(["docker","exec","-i","vf-pg","psql","-U","postgres","-v","ON_ERROR_STOP=1","-tAF","\t","-c",sql],capture_output=True,text=True)
    if r.returncode: raise SystemExit(r.stderr)
    return r.stdout
psql("drop text search dictionary if exists vf_hun_nostop; create text search dictionary vf_hun_nostop (template=ispell, dictfile=en_us, afffile=en_us)")
out=psql("select w, coalesce(array_to_string(ts_lexize('vf_hun_nostop', w),'|'),'<null>') from vw")
pg={l.split('\t')[0]:l.split('\t')[1] for l in out.splitlines() if l}
es={}
for i in range(0,len(V),2000):
    chunk=V[i:i+2000]
    body=json.dumps({"tokenizer":"keyword","filter":[{"type":"hunspell","locale":"en_US"}],"text":chunk}).encode()
    toks=json.load(urllib.request.urlopen(urllib.request.Request("http://localhost:55804/_analyze",body,{"content-type":"application/json"})))["tokens"]
    by={}
    for t in toks:
        idx,rem=divmod(t["position"],101); assert rem==0
        by.setdefault(idx,[]).append(t["token"])
    for j,w in enumerate(chunk): es[w]=by.get(j,[])
pgl={w:([] if v in ('<null>','') else v.split('|')) for w,v in pg.items()}
json.dump({"pg":pgl,"es":es},open("hun_live.json","w"))
diff=[w for w in V if sorted(pgl[w] or [w])!=sorted(es[w] or [w])]
print("differ",len(diff),"multi pg",sum(len(v)>1 for v in pgl.values()),"multi es",sum(len(v)>1 for v in es.values()))
for w in ("dying","news","better","was","went","mice","studies","running","children","best"): print(w, "pg",pgl.get(w, psql(f"select ts_lexize('vf_hun_nostop','{w}')").strip()), "es", es.get(w))
print("pg null (unknown word):",sum(1 for v in pg.values() if v=='<null>'), "pg empty array:", sum(1 for v in pg.values() if v==''))

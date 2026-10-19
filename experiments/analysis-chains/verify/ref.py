import json, sys, importlib.metadata as md
WORDS = json.load(open("live_stems.json"))["pg18.1 english_stem"].keys()
kind = sys.argv[1]
if kind == "snowballstemmer":
    import snowballstemmer
    try:
        import Stemmer; sys.exit("PyStemmer importable, contaminated")
    except ImportError: pass
    s = snowballstemmer.stemmer("english"); res = {w: s.stemWord(w) for w in WORDS}
    ver = md.version("snowballstemmer")
elif kind == "pystemmer":
    import Stemmer
    s = Stemmer.Stemmer("english"); res = {w: s.stemWord(w) for w in WORDS}
    p = Stemmer.Stemmer("porter"); res_p = {w: p.stemWord(w) for w in WORDS}
    ver = md.version("PyStemmer"); res = {"english": res, "porter": res_p}
elif kind == "nltk":
    from nltk.stem.porter import PorterStemmer
    res = {m: {w: PorterStemmer(mode=m).stem(w) for w in WORDS} for m in ("ORIGINAL_ALGORITHM","MARTIN_EXTENSIONS","NLTK_EXTENSIONS")}
    ver = md.version("nltk")
print(json.dumps({"kind":kind,"version":ver,"res":res}))

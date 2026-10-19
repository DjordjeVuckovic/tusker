import json, sys, importlib.metadata as md
V = sorted(json.load(open('/Users/djordjevuckovic/projects/news-hunter/experiments/analysis-chains/out/vocab.json')))
kind=sys.argv[1]
if kind=="sbs":
    try:
        import Stemmer; sys.exit("contaminated")
    except ImportError: pass
    import snowballstemmer; s=snowballstemmer.stemmer("english"); res={w:s.stemWord(w) for w in V}; ver=md.version("snowballstemmer")
elif kind=="pys":
    import Stemmer; s=Stemmer.Stemmer("english"); res={w:s.stemWord(w) for w in V}; ver=md.version("PyStemmer")
elif kind=="nltk":
    from nltk.stem.porter import PorterStemmer as P
    res={m:{w:P(mode=m).stem(w) for w in V} for m in ("ORIGINAL_ALGORITHM","MARTIN_EXTENSIONS","NLTK_EXTENSIONS")}; ver=md.version("nltk")
json.dump({"version":ver,"res":res},open(sys.argv[2],"w"))
print(kind,ver)

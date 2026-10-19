import json,sys,importlib.metadata as md
V=sorted(json.load(open('/Users/djordjevuckovic/projects/news-hunter/experiments/analysis-chains/out/vocab.json')))
import Stemmer; s=Stemmer.Stemmer("porter"); json.dump({"version":md.version("PyStemmer"),"res":{w:s.stemWord(w) for w in V}},open(sys.argv[1],"w"))

import json,sys
import nltk; nltk.data.path.insert(0,'/Users/djordjevuckovic/projects/news-hunter/experiments/analysis-chains/deps/nltk_data')
from nltk.corpus import wordnet as wn
words=json.load(open(sys.argv[1])); print(wn.get_version(), file=sys.stderr)
json.dump({w:(wn.morphy(w) or w) for w in words},open(sys.argv[2],"w"))

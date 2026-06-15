# Text Processing Fundamentals for Full-Text Search

> **Academic Context**: This document explains text processing concepts (tokenization, stemming, lemmatization, stop words) for the master thesis "PostgreSQL as a Search Engine" - understanding how raw text is transformed into searchable tokens.

## Table of Contents
1. [Introduction](#introduction)
2. [Tokenization](#tokenization)
3. [Stop Words](#stop-words)
4. [Stemming](#stemming)
5. [Lemmatization](#lemmatization)
6. [Processing Pipeline](#processing-pipeline)
7. [PostgreSQL Implementation](#postgresql-implementation)
8. [Elasticsearch Implementation](#elasticsearch-implementation)
9. [Practical Examples](#practical-examples)
10. [Configuration Options](#configuration-options)
11. [Performance Considerations](#performance-considerations)
12. [Multilingual Challenges](#multilingual-challenges)

---

## Introduction

Text processing is the foundation of full-text search engines. Before search can happen, raw text must be transformed into a standardized, searchable format. This process involves:

1. **Breaking text into meaningful units** (tokenization)
2. **Removing noise words** (stop word removal)
3. **Normalizing word forms** (stemming/lemmatization)
4. **Creating searchable index** (inverted index or tsvector)

**Why it matters for Tusker:**
- Enables matching "climate" with "climatic", "climate change", "climatology"
- Improves search relevance by focusing on meaningful content
- Reduces index size and improves query performance
- Handles user queries regardless of word forms or typos

---

## Tokenization

**Definition**: The process of breaking text into individual units (tokens) - typically words or phrases.

### How Tokenization Works

```
Input: "Climate change is affecting global weather patterns"
Tokens: ["Climate", "change", "is", "affecting", "global", "weather", "patterns"]
```

### Tokenization Approaches

#### 1. Whitespace Tokenization
```go
// Simple approach
tokens := strings.Split(text, " ")
// Problem: "don't" becomes ["don't"], punctuation attached
```

#### 2. Punctuation-Aware Tokenization
```
Input: "U.S. climate change, don't you think?"
Tokens: ["U.S.", "climate", "change", "don't", "you", "think"]
```

#### 3. Unicode-Aware Tokenization
```
Input: "Café München 気候変化"
Tokens: ["Café", "München", "気候", "変化"]
```

#### 4. N-gram Tokenization
```
Input: "climate"
Bi-grams: ["cl", "li", "im", "ma", "at", "te"]
Tri-grams: ["cli", "lim", "ima", "mat", "ate"]
```

### Tokenization in Search Systems

#### PostgreSQL (tsvector)
```sql
SELECT to_tsvector('english', 'Climate change is affecting global weather');
-- Result: 'affect':5 'chang':2 'climat':1 'global':6 'weather':7
```

#### Elasticsearch (Standard Tokenizer)
```json
{
  "tokenizer": "standard",
  "filter": ["lowercase", "stop"]
}
// "Climate change" → ["climate", "change"]
```

### Why Tokenization Matters

**Good tokenization enables:**
- Accurate phrase matching
- Proper handling of contractions and punctuation
- Multilingual text processing
- Efficient indexing and searching

**Poor tokenization causes:**
- Missed matches due to punctuation
- Incorrect phrase boundaries
- Language-specific issues
- Reduced search quality

---

## Stop Words

**Definition**: Common words that carry little semantic meaning and are typically ignored in search queries.

### Common Stop Words Lists

#### English Stop Words
```
Articles: a, an, the
Prepositions: in, on, at, by, for, to, from, with, without
Conjunctions: and, or, but, nor, yet, so
Pronouns: he, she, it, they, we, you, I
Auxiliary verbs: is, am, are, was, were, be, been, being
Others: that, this, these, those, what, which, who
```

#### Multilingual Examples
```
Spanish: el, la, los, las, un, una, y, o, pero, sin
French: le, la, les, un, une, et, ou, mais, sans
German: der, die, das, ein, eine, und, oder, aber, ohne
```

### Stop Word Removal in Practice

#### Before Removal
```
Query: "The climate change and global warming effects"
Tokens: ["The", "climate", "change", "and", "global", "warming", "effects"]
```

#### After Removal
```
Tokens: ["climate", "change", "global", "warming", "effects"]
Index Size: ~30% smaller
Search Focus: On meaningful content only
```

### Stop Word Strategies

#### 1. Aggressive Removal
```sql
-- PostgreSQL with default stop words
SELECT to_tsvector('english', 'The effects of climate change');
-- Result: 'effect':2 'climat':4 'chang':5
```

#### 2. Conservative Removal
```sql
-- Custom configuration with minimal stop words
SELECT to_tsvector('simple', 'The effects of climate change');
-- Result: 'the':1 'effects':2 'of':3 'climate':4 'change':5
```

#### 3. No Stop Words
```json
// Elasticsearch configuration
{
  "analyzer": {
    "no_stop": {
      "tokenizer": "standard",
      "filter": ["lowercase"]  // No stop word filter
    }
  }
}
```

### Benefits and Trade-offs

#### Benefits
- **Reduced Index Size**: 20-40% smaller indexes
- **Faster Queries**: Fewer terms to match
- **Better Relevance**: Focus on content words
- **Lower Memory Usage**: Less RAM for caching

#### Trade-offs
- **Lost Phrase Meaning**: "To be or not to be" becomes empty
- **Context Loss**: Some stop words carry meaning in specific contexts
- **Query Complexity**: Users must understand stop word behavior
- **Language Specificity**: Different stop words for different languages

---

## Stemming

**Definition**: The process of reducing words to their root form by removing suffixes and prefixes using algorithmic rules.

### How Stemming Works

#### Porter Stemmer Algorithm
```
running → run + ning → run
climate → climat + e → climat
climatic → climat + ic → climat
studies → studi + es → studi
studying → studi + ing → studi
```

#### Stemming Examples
```
Word      | Stemmed | Notes
----------|---------|-----------------
running   | run     | Removes -ing suffix
runs      | run     | Removes -s suffix
ran       | ran     | Irregular - may not change
climate   | climat  | Removes -e suffix
climatic  | climat  | Removes -ic suffix
climatology| climat  | Removes -ology suffix
studies   | studi   | Removes -ies → -y + i
studying  | studi   | Removes -ing suffix
```

### Popular Stemming Algorithms

#### 1. Porter Stemmer
- **Characteristics**: Fast, widely used, English-focused
- **Complexity**: O(n) where n is word length
- **Usage**: PostgreSQL's default English stemmer

```sql
SELECT to_tsvector('english', 'running studies climate');
-- Result: 'run':1 'studi':2 'climat':3
```

#### 2. Snowball Stemmer
- **Characteristics**: Multi-language, improved accuracy
- **Languages**: English, Spanish, French, German, Russian, etc.
- **Usage**: Elasticsearch's snowball filter

```json
{
  "filter": [
    {
      "type": "snowball",
      "language": "English"
    }
  ]
}
```

#### 3. Paice-Husk Stemmer
- **Characteristics**: More aggressive, higher recall
- **Usage**: Specialized applications needing maximum recall

### Stemming in Search Systems

#### PostgreSQL Implementation
```sql
-- Automatic stemming in tsvector creation
CREATE TABLE articles (
    content TEXT,
    search_vector TSVECTOR
);

-- Update search vector with stemming
UPDATE articles 
SET search_vector = to_tsvector('english', content);

-- Query with stemming
SELECT * FROM articles 
WHERE search_vector @@ plainto_tsquery('english', 'running studies');
-- Matches: "run", "running", "ran", "studies", "studying", etc.
```

#### Elasticsearch Implementation
```json
{
  "analyzer": {
    "english_stemmed": {
      "tokenizer": "standard",
      "filter": [
        "lowercase",
        "stop",
        {
          "type": "snowball",
          "language": "English"
        }
      ]
    }
  }
}
```

### Stemming Benefits and Limitations

#### Benefits
- **Increased Recall**: Finds documents regardless of word form
- **Reduced Vocabulary**: Smaller indexes, faster searches
- **Language Independence**: Algorithmic approach, no dictionaries needed
- **Fast Processing**: Computationally inexpensive

#### Limitations
- **Non-words**: "climat" instead of "climate"
- **Over-stemming**: "university" → "univers"
- **Under-stemming**: "ran" → "ran" (should be "run")
- **Language Specific**: Different rules needed for each language

---

## Lemmatization

**Definition**: The process of reducing words to their dictionary form (lemma) using vocabulary and morphological analysis.

### How Lemmatization Works

#### Part-of-Speech Analysis
```
Word: "running"
POS: Verb (present participle)
Lemma: "run"

Word: "studies"  
POS: Noun (plural)
Lemma: "study"

Word: "better"
POS: Adjective (comparative)
Lemma: "good"

Word: "was"
POS: Verb (past tense)
Lemma: "be"
```

#### Lemmatization vs Stemming Comparison

| Word      | Stemming | Lemmatization | Difference          |
|-----------|-----------|---------------|---------------------|
| running   | run       | run           | Same                |
| studies   | studi     | study         | Real word vs non-word|
| studying  | studi     | study         | Real word vs non-word|
| better    | better    | good          | Different base form  |
| best      | best      | good          | Different base form  |
| was       | was       | be            | Different base form  |
| were      | were      | be            | Different base form  |
| climate   | climat    | climate       | Real word vs non-word|

### Lemmatization Process

#### 1. Tokenization
```
Input: "The running dogs were studying better"
Tokens: ["The", "running", "dogs", "were", "studying", "better", "methods"]
```

#### 2. Part-of-Speech Tagging
```
"The" → Determinant
"running" → Verb (present participle)
"dogs" → Noun (plural)
"were" → Verb (past tense)
"studying" → Verb (present participle)
"better" → Adjective (comparative)
"methods" → Noun (plural)
```

#### 3. Dictionary Lookup
```
running (verb) → run
dogs (noun, plural) → dog
were (verb, past) → be
studying (verb, present) → study
better (adjective, comparative) → good
methods (noun, plural) → method
```

#### 4. Final Result
```
Lemmas: ["run", "dog", "be", "study", "good", "method"]
```

### Lemmatization in Practice

#### PostgreSQL Limitations
```sql
-- PostgreSQL primarily uses stemming, not lemmatization
SELECT to_tsvector('english', 'better best was were');
-- Result: 'better':1 'best':2 'was':3 'were':4
-- Note: "better" and "best" remain unchanged
```

#### Elasticsearch Support
```json
{
  "filter": [
    {
      "type": "stemmer",
      "language": "english"
    },
    {
      "type": "possessive_stemmer"
    }
  ]
  // Note: Elasticsearch also primarily uses stemming
  // True lemmatization requires specialized plugins
}
```

### Lemmatization Benefits and Challenges

#### Benefits
- **Real Words**: Produces actual dictionary entries
- **Higher Precision**: More accurate word normalization
- **Better User Experience**: Results are more intuitive
- **Context Awareness**: Considers part-of-speech

#### Challenges
- **Computational Cost**: Requires POS tagging and dictionary lookups
- **Language Dependencies**: Needs extensive lexical databases
- **Complex Implementation**: Morphological analysis is complex
- **Memory Usage**: Large dictionaries required

---

## Processing Pipeline

### Complete Text Processing Workflow

#### 1. Input Text
```
Raw Document: "The running dogs were studying climate change methods better than before."
```

#### 2. Character Filtering
```
Remove HTML, normalize Unicode, handle special characters
Result: "The running dogs were studying climate change methods better than before"
```

#### 3. Tokenization
```
Break into individual words
Tokens: ["The", "running", "dogs", "were", "studying", "climate", "change", "methods", "better", "than", "before"]
```

#### 4. Case Normalization
```
Convert to lowercase
Tokens: ["the", "running", "dogs", "were", "studying", "climate", "change", "methods", "better", "than", "before"]
```

#### 5. Stop Word Removal
```
Remove common words
Tokens: ["running", "dogs", "studying", "climate", "change", "methods", "better", "before"]
```

#### 6. Stemming/Lemmatization
```
Reduce to root forms
Tokens: ["run", "dog", "studi", "climat", "chang", "method", "better", "befor"]
```

#### 7. Index Storage
```
Store in searchable format
PostgreSQL: tsvector with positions
Elasticsearch: Inverted index with term frequencies
```

### Query Processing Pipeline

#### User Query
```
Input: "running climate studies"
```

#### Same Pipeline as Document Processing
```
1. Tokenize: ["running", "climate", "studies"]
2. Lowercase: ["running", "climate", "studies"]  
3. Stop removal: ["running", "climate", "studies"]
4. Stemming: ["run", "climat", "studi"]
```

#### Final Search Query
```
PostgreSQL: plainto_tsquery('run & climat & studi')
Elasticsearch: bool query with must clauses for each term
```

---

## PostgreSQL Implementation

### Text Search Configuration

#### Built-in Configurations
```sql
-- View available configurations
SELECT cfgname FROM pg_ts_config;

-- Common configurations
'english'    -- English stemming and stop words
'simple'      -- No stemming, minimal processing  
'spanish'     -- Spanish language support
'french'      -- French language support
'german'      -- German language support
```

#### Custom Configuration
```sql
-- Create custom text search configuration
CREATE TEXT SEARCH CONFIGURATION custom_english (
    COPY = english
);

-- Modify stop word list
ALTER TEXT SEARCH CONFIGURATION custom_english 
    DROP MAPPING FOR asciiword;

-- Add custom mappings
ALTER TEXT SEARCH CONFIGURATION custom_english
    ADD MAPPING FOR asciiword WITH simple;
```

### tsvector Creation

#### Automatic Processing
```sql
-- Full processing pipeline
SELECT to_tsvector('english', 'The running dogs studied climate change');
-- Result: 'chang':5 'climat':4 'dog':2 'run':1 'studi':3
```

#### Step-by-Step Processing
```sql
-- 1. Tokenization only
SELECT tsvector_to_array(to_tsvector('simple', 'running dogs'));
-- Result: {running,dogs}

-- 2. With stemming
SELECT tsvector_to_array(to_tsvector('english', 'running dogs'));
-- Result: {run,dog}

-- 3. With stop words
SELECT tsvector_to_array(to_tsvector('english', 'the running dogs'));
-- Result: {run,dog}  -- "the" removed
```

### Query Processing

#### plainto_tsquery (AND Logic)
```sql
SELECT plainto_tsquery('english', 'running climate studies');
-- Result: 'run' & 'climat' & 'studi'

-- Matches documents containing ALL terms
```

#### to_tsquery (Explicit Logic)
```sql
SELECT to_tsquery('english', 'running & (climate | weather) & !politics');
-- Result: 'run' & ('climat' | 'weather') & !'polit'

-- Supports complex boolean expressions
```

#### websearch_to_tsquery (Web-style)
```sql
SELECT websearch_to_tsquery('english', 'running climate -politics "climate change"');
-- Result: 'run' & 'climat' & !'polit' & 'climat' <-> 'chang'

-- Supports +, -, "", AND, OR, NOT operators
```

### Performance Optimization

#### Index Types
```sql
-- GIN index for full-text search
CREATE INDEX idx_articles_search ON articles 
USING GIN(search_vector);

-- GiST index for faster updates
CREATE INDEX idx_articles_search_gist ON articles 
USING GIST(search_vector);
```

#### Configuration Tuning
```sql
-- Set default configuration
SET default_text_search_config = 'pg_catalog.english';

-- Adjust ranking normalization
SET ts_rank_normalization = 1;  -- Normalize by document length
```

---

## Elasticsearch Implementation

### Analyzer Configuration

#### Standard Analyzer
```json
{
  "analyzer": {
    "standard": {
      "tokenizer": "standard",
      "filter": [
        "lowercase",
        "stop"
      ]
    }
  }
}
```

#### Custom Analyzer with Stemming
```json
{
  "settings": {
    "analysis": {
      "analyzer": {
        "news_analyzer": {
          "tokenizer": "standard",
          "filter": [
            "lowercase",
            {
              "type": "stop",
              "stopwords": "_english_"
            },
            {
              "type": "snowball",
              "language": "English"
            }
          ]
        }
      }
    }
  }
}
```

#### Tokenizer Options

##### Standard Tokenizer
```json
{
  "tokenizer": "standard",
  "max_token_length": 255
}
// Splits on word boundaries, handles most punctuation
```

##### Letter Tokenizer
```json
{
  "tokenizer": "letter"
}
// Only keeps letters, removes all numbers and punctuation
```

##### Whitespace Tokenizer
```json
{
  "tokenizer": "whitespace"
}
// Splits on any whitespace, preserves punctuation
```

### Filter Chain

#### Common Filters
```json
{
  "filter": [
    "lowercase",                    // Convert to lowercase
    {
      "type": "stop",              // Remove stop words
      "stopwords": "_english_"
    },
    {
      "type": "snowball",          // Stemming
      "language": "English"
    },
    {
      "type": "stemmer_override",   // Custom stemming rules
      "rules": [
        "climate => climat",
        "study => studi"
      ]
    }
  ]
}
```

#### Custom Stop Words
```json
{
  "type": "stop",
  "stopwords": [
    "the", "a", "an", "and", "or", "but",
    "news", "article", "report"  // Domain-specific stop words
  ]
}
```

### Field Mapping

#### Text Field Configuration
```json
{
  "mappings": {
    "properties": {
      "title": {
        "type": "text",
        "analyzer": "news_analyzer",
        "search_analyzer": "news_analyzer"
      },
      "content": {
        "type": "text", 
        "analyzer": "news_analyzer",
        "search_analyzer": "news_analyzer"
      },
      "keywords": {
        "type": "keyword"  // No analysis, exact matching
      }
    }
  }
}
```

### Multi-language Support

#### Language-specific Analyzers
```json
{
  "analyzer": {
    "english": {
      "tokenizer": "standard",
      "filter": ["lowercase", "english_stop", "english_stemmer"]
    },
    "spanish": {
      "tokenizer": "standard", 
      "filter": ["lowercase", "spanish_stop", "spanish_stemmer"]
    },
    "french": {
      "tokenizer": "standard",
      "filter": ["lowercase", "french_stop", "french_stemmer"]
    }
  }
}
```

#### Language Detection
```json
{
  "filter": [
    {
      "type": "stemmer",
      "language": "_stemmer_language_"
    }
  ]
}
// Automatically detects document language
```

---

## Practical Examples

### News Search Examples

#### Example 1: Climate Change Query
```
User Query: "climate change renewable energy studies"

Processing Steps:
1. Tokenize: ["climate", "change", "renewable", "energy", "studies"]
2. Lowercase: ["climate", "change", "renewable", "energy", "studies"]
3. Stop removal: ["climate", "change", "renewable", "energy", "studies"]
4. Stemming: ["climat", "chang", "renew", "energi", "studi"]

PostgreSQL Query: 'climat' & 'chang' & 'renew' & 'energi' & 'studi'
Elasticsearch: bool query with must clauses for each term
```

#### Example 2: Political News Query
```
User Query: "Trump election fraud investigation"

Processing Steps:
1. Tokenize: ["Trump", "election", "fraud", "investigation"]
2. Lowercase: ["trump", "election", "fraud", "investigation"]
3. Stop removal: ["trump", "election", "fraud", "investigation"]
4. Stemming: ["trump", "elect", "fraud", "investig"]

Matches documents containing:
- "Trump", "Trump's"
- "election", "elections", "electoral"
- "fraud", "fraudulent"
- "investigation", "investigate", "investigative"
```

#### Example 3: Academic Research Query
```
User Query: "machine learning artificial intelligence algorithms"

Processing Steps:
1. Tokenize: ["machine", "learning", "artificial", "intelligence", "algorithms"]
2. Lowercase: ["machine", "learning", "artificial", "intelligence", "algorithms"]
3. Stop removal: ["machine", "learning", "artificial", "intelligence", "algorithms"]
4. Stemming: ["machin", "learn", "artifici", "intellig", "algorithm"]

Matches variations like:
- "machine", "machinery", "machines"
- "learning", "learn", "learned"
- "artificial", "artificially"
- "intelligence", "intelligent"
- "algorithms", "algorithmic"
```

### Document Processing Examples

#### News Article Processing
```
Original: "Scientists have discovered that climate change is accelerating faster than previously thought. The study reveals alarming trends in global temperature patterns."

Processed Tokens:
['scientist', 'discov', 'climat', 'chang', 'acceler', 'fast', 'previous', 'thought', 'studi', 'reveal', 'alarm', 'trend', 'global', 'temperatur', 'pattern']

Index Entry:
Document ID → [scientist, discov, climat, chang, acceler, fast, previous, thought, studi, reveal, alarm, trend, global, temperatur, pattern]
```

#### Multilingual Document
```
Original: "Climate change el cambio climático le changement climatique"

English Processing: ['climat', 'chang']
Spanish Processing: ['cambi', 'climat']  
French Processing: ['chang', 'climat']

Combined: ['climat', 'chang', 'cambi', 'chang', 'climat']
Unique: ['climat', 'chang', 'cambi']
```

---

## Configuration Options

### PostgreSQL Configuration

#### Text Search Configuration
```sql
-- View current configuration
SHOW default_text_search_config;

-- Set default configuration
SET default_text_search_config = 'pg_catalog.english';

-- Create custom configuration
CREATE TEXT SEARCH CONFIGURATION news_config (
    COPY = english
);

-- Modify stop words
ALTER TEXT SEARCH CONFIGURATION news_config
    ALTER MAPPING FOR asciiword WITH english_stem;
```

#### Dictionary Configuration
```sql
-- View available dictionaries
SELECT dictname FROM pg_ts_dict;

-- Create custom dictionary
CREATE TEXT SEARCH DICTIONARY custom_dict (
    TEMPLATE = simple,
    STOPWORDS = 'custom_stopwords.txt'
);

-- Use custom dictionary
ALTER TEXT SEARCH CONFIGURATION news_config
    ALTER MAPPING FOR asciiword WITH custom_dict;
```

#### Ranking Configuration
```sql
-- Normalization options
-- 0: No normalization
-- 1: Divide by (1 + log(document length))
-- 2: Divide by document length
-- 4: Divide by mean harmonic distance
-- 8: Divide by number of unique words
-- 16: Divide by (1 + log(unique words))

SELECT ts_rank(search_vector, query, 1);  -- Length normalized
SELECT ts_rank_cd(search_vector, query, 4); -- Cover density with distance
```

### Elasticsearch Configuration

#### Analyzer Settings
```json
{
  "settings": {
    "analysis": {
      "analyzer": {
        "news_analyzer": {
          "type": "custom",
          "tokenizer": "standard",
          "filter": [
            "lowercase",
            "custom_stop_words",
            "custom_stemmer"
          ]
        }
      },
      "filter": {
        "custom_stop_words": {
          "type": "stop",
          "stopwords": [
            "the", "a", "an", "and", "or", "but",
            "said", "says", "according", "report"
          ]
        },
        "custom_stemmer": {
          "type": "stemmer",
          "language": "English",
          "name": "minimal_english"
        }
      }
    }
  }
}
```

#### Field-specific Configuration
```json
{
  "mappings": {
    "properties": {
      "title": {
        "type": "text",
        "analyzer": "title_analyzer",      // Less aggressive stemming
        "search_quote_analyzer": "quote_analyzer"
      },
      "content": {
        "type": "text", 
        "analyzer": "content_analyzer",    // Standard processing
        "search_analyzer": "content_analyzer"
      },
      "author": {
        "type": "text",
        "analyzer": "name_analyzer",       // Preserve names
        "search_analyzer": "name_analyzer"
      }
    }
  }
}
```

#### Performance Settings
```json
{
  "settings": {
    "index": {
      "max_result_window": 50000,
      "analysis": {
        "analyzer": {
          "fast_analyzer": {
            "tokenizer": "keyword",
            "filter": ["lowercase"]
          }
        }
      }
    }
  }
}
```

---

## Performance Considerations

### Index Size Impact

#### Stop Word Removal Impact
```
Without stop words:
- Index size: 100MB
- Query time: 50ms
- Memory usage: 200MB

With stop words:
- Index size: 65MB (-35%)
- Query time: 35ms (-30%)
- Memory usage: 130MB (-35%)
```

#### Stemming Impact
```
Without stemming:
- Unique terms: 500,000
- Index size: 80MB
- Recall: 70%

With stemming:
- Unique terms: 200,000 (-60%)
- Index size: 45MB (-44%)
- Recall: 85% (+15%)
```

### Query Performance

#### Tokenization Speed
```
Standard tokenizer: 1,000 docs/sec
Letter tokenizer: 800 docs/sec
Whitespace tokenizer: 1,200 docs/sec
N-gram tokenizer: 600 docs/sec
```

#### Stemming Performance
```
Porter stemmer: 5,000 words/sec
Snowball stemmer: 3,000 words/sec
No stemming: 10,000 words/sec
```

#### Memory Usage
```
PostgreSQL tsvector:
- Base: 100 bytes per document
- With stemming: 70 bytes per document
- With stop words: 45 bytes per document

Elasticsearch inverted index:
- Base: 150 bytes per document
- With stemming: 100 bytes per document
- With stop words: 65 bytes per document
```

### Optimization Strategies

#### 1. Choose Right Configuration
```sql
-- For news search: moderate stemming, standard stop words
-- For academic search: minimal stemming, preserve terms
-- for legal search: no stemming, preserve all terms
```

#### 2. Field-specific Processing
```json
{
  "title": {
    "analyzer": "title_analyzer"      // Less aggressive
  },
  "content": {
    "analyzer": "content_analyzer"    // Standard processing
  },
  "keywords": {
    "analyzer": "keyword_analyzer"    // Exact match
  }
}
```

#### 3. Caching Strategies
```sql
-- Materialized processed text
ALTER TABLE articles ADD COLUMN processed_content TSVECTOR;
UPDATE articles SET processed_content = to_tsvector('english', content);
CREATE INDEX idx_processed ON articles USING GIN(processed_content);
```

#### 4. Batch Processing
```sql
-- Efficient bulk processing
INSERT INTO articles (content, search_vector)
SELECT content, to_tsvector('english', content)
FROM raw_articles
WHERE processed = false;
```

---

## Multilingual Challenges

### Language Detection

#### Automatic Detection
```python
# Python example using langdetect
from langdetect import detect

text = "El cambio climático es un problema global"
language = detect(text)  # Returns 'es'
```

#### Elasticsearch Language Detection
```json
{
  "filter": [
    {
      "type": "stemmer",
      "language": "_stemmer_language_"
    }
  ]
}
```

### Language-specific Processing

#### English Challenges
```
- Irregular verbs: run/ran, go/went, be/was/were
- Contractions: don't, can't, won't
- Compound words: climate change, global warming
- Idioms: "kick the bucket" (literal vs figurative)
```

#### Spanish Challenges
```
- Verb conjugations: cambio/cambian/cambió
- Gender agreement: el/la, los/las
- Accented characters: climático/climatico
- Formal/informal: tú/usted
```

#### French Challenges
```
- Liaison: les amis (pronounced "lezami")
- Accents: été/ete, climat/climat
- Gender: le/la, un/une
- Verb conjugations: est/sont/était/sera
```

#### German Challenges
```
- Compound words: Klimawandel (climate change)
- Case markings: der/die/das/den/dem
- Umlauts: kühl/kuehl (cool/cold)
- Long words: Donaudampfschifffahrtsgesellschaftskapitän
```

### Multilingual Configuration

#### PostgreSQL Multilingual
```sql
-- Language-specific configurations
CREATE TEXT SEARCH CONFIGURATION spanish (
    COPY = spanish
);

CREATE TEXT SEARCH CONFIGURATION french (
    COPY = french  
);

-- Language detection function
CREATE OR REPLACE FUNCTION detect_language(text) 
RETURNS TEXT AS $$
BEGIN
    -- Simple heuristic based on character sets
    IF text ~ '[ñáéíóúü]' THEN RETURN 'spanish';
    ELSIF text ~ '[àâäæçéèêëïîôùûœ]' THEN RETURN 'french';
    ELSIF text ~ '[äöüß]' THEN RETURN 'german';
    ELSE RETURN 'english';
    END IF;
END;
$$ LANGUAGE plpgsql;
```

#### Elasticsearch Multilingual
```json
{
  "settings": {
    "analysis": {
      "analyzer": {
        "multilingual": {
          "tokenizer": "icu_tokenizer",
          "filter": [
            "icu_folding",
            "icu_normalizer",
            "multilingual_stemmer"
          ]
        }
      }
    }
  }
}
```

### Cross-language Search

#### Query Translation
```sql
-- Translate query to multiple languages
WITH translated_queries AS (
    SELECT 
        plainto_tsquery('english', query) as english_query,
        plainto_tsquery('spanish', translate_to_spanish(query)) as spanish_query,
        plainto_tsquery('french', translate_to_french(query)) as french_query
    FROM user_queries
)
SELECT * FROM articles 
WHERE search_vector @@ english_query
   OR search_vector @@ spanish_query  
   OR search_vector @@ french_query;
```

#### Universal Stemming
```json
{
  "filter": [
    {
      "type": "stemmer",
      "language": "minimal_english"  // More conservative
    }
  ]
}
```

---

## Conclusion

Text processing is the foundation of effective full-text search systems. For the Tusker project's comparison between PostgreSQL and Elasticsearch:

### Key Takeaways

1. **Tokenization** is the first critical step - poor tokenization breaks everything else
2. **Stop words** significantly impact performance and index size
3. **Stemming** increases recall but may reduce precision
4. **Lemmatization** provides better quality but at higher computational cost
5. **Configuration** must be tuned for specific domains and languages

### PostgreSQL vs Elasticsearch

| Aspect | PostgreSQL | Elasticsearch |
|--------|------------|----------------|
| **Stemming** | Porter/Snowball | Snowball + custom |
| **Stop Words** | Configurable lists | Highly configurable |
| **Languages** | Built-in configs | Extensive support |
| **Customization** | SQL-level | JSON configuration |
| **Performance** | Faster for simple cases | Better for complex pipelines |

### Recommendations for Tusker

1. **Use standard configurations** for news domain
2. **Implement moderate stemming** for balance of recall/precision
3. **Remove domain-specific stop words** ("said", "report", "according")
4. **Consider multilingual support** for international news
5. **Monitor performance** impact of different configurations

This understanding of text processing fundamentals enables accurate comparison of PostgreSQL and Elasticsearch search capabilities for the master thesis research.

---

*Last updated: 2025-11-08*
*Master Thesis: "PostgreSQL as a Search Engine"*

#!/bin/bash
# Tusker API - Example Requests
# Usage: chmod +x curl_examples.sh && ./curl_examples.sh

API_BASE="http://localhost:8080"

echo "================================================"
echo "Tusker Search API Examples"
echo "================================================"

# ============================================================================
# SIMPLE SEARCH (GET) - Cacheable, bookmarkable
# ============================================================================

echo -e "\n[1] Simple text search"
curl -X GET "${API_BASE}/v1/articles/search?q=climate%20change&size=5" \
  -H "Accept: application/json" | jq '.'

echo -e "\n[2] Search with language"
curl -X GET "${API_BASE}/v1/articles/search?q=promene%20klime&lang=serbian&size=5" \
  -H "Accept: application/json" | jq '.'

echo -e "\n[3] Search with pagination"
# First get initial results
RESPONSE=$(curl -s -X GET "${API_BASE}/v1/articles/search?q=renewable%20energy&size=5")
CURSOR=$(echo "$RESPONSE" | jq -r '.next_cursor')
echo "First page results..."
echo "$RESPONSE" | jq '{total_matches, has_more, hits: (.hits | length)}'

# Then get next page
if [ "$CURSOR" != "null" ]; then
  echo -e "\nSecond page results..."
  curl -X GET "${API_BASE}/v1/articles/search?q=renewable%20energy&size=5&cursor=${CURSOR}" \
    -H "Accept: application/json" | jq '{total_matches, has_more, hits: (.hits | length)}'
fi

# ============================================================================
# STRUCTURED SEARCH (POST) - Complex queries
# ============================================================================

echo -e "\n[4] Match Query - Search in title only"
curl -X POST "${API_BASE}/v1/articles/_search" \
  -H "Content-Type: application/json" \
  -d '{
    "size": 5,
    "query": {
      "match": {
        "field": "title",
        "query": "climate change",
        "operator": "and",
        "language": "english"
      }
    }
  }' | jq '.'

echo -e "\n[5] Match Query - With fuzziness (typo tolerance)"
curl -X POST "${API_BASE}/v1/articles/_search" \
  -H "Content-Type: application/json" \
  -d '{
    "size": 5,
    "query": {
      "match": {
        "field": "content",
        "query": "climte changge",
        "fuzziness": "AUTO"
      }
    }
  }' | jq '.'

echo -e "\n[6] Multi-Match Query - Search across multiple fields"
curl -X POST "${API_BASE}/v1/articles/_search" \
  -H "Content-Type: application/json" \
  -d '{
    "size": 5,
    "query": {
      "multi_match": {
        "query": "renewable energy",
        "fields": ["title", "description", "content"],
        "operator": "or"
      }
    }
  }' | jq '.'

echo -e "\n[7] Multi-Match Query - With field weights (boost title 3x)"
curl -X POST "${API_BASE}/v1/articles/_search" \
  -H "Content-Type: application/json" \
  -d '{
    "size": 5,
    "query": {
      "multi_match": {
        "query": "climate change policy",
        "fields": ["title", "description", "content"],
        "field_weights": {
          "title": 3.0,
          "description": 2.0,
          "content": 1.0
        },
        "operator": "and"
      }
    }
  }' | jq '.'

echo -e "\n[8] Multi-Match Query - Serbian language"
curl -X POST "${API_BASE}/v1/articles/_search" \
  -H "Content-Type: application/json" \
  -d '{
    "size": 5,
    "query": {
      "multi_match": {
        "query": "obnovljiva energija",
        "fields": ["title", "description", "content"],
        "language": "serbian",
        "operator": "or"
      }
    }
  }' | jq '.'

# ============================================================================
# PAGINATION EXAMPLES
# ============================================================================

echo -e "\n[9] Pagination - Get first page and use cursor for next"
curl -X POST "${API_BASE}/v1/articles/_search" \
  -H "Content-Type: application/json" \
  -d '{
    "size": 3,
    "query": {
      "match": {
        "field": "content",
        "query": "technology"
      }
    }
  }' > /tmp/page1.json

echo "Page 1:"
jq '{next_cursor, has_more, total_matches, results: (.hits | length)}' /tmp/page1.json

NEXT_CURSOR=$(jq -r '.next_cursor' /tmp/page1.json)
if [ "$NEXT_CURSOR" != "null" ]; then
  echo -e "\nPage 2:"
  curl -X POST "${API_BASE}/v1/articles/_search" \
    -H "Content-Type: application/json" \
    -d "{
      \"size\": 3,
      \"cursor\": \"$NEXT_CURSOR\",
      \"query\": {
        \"match\": {
          \"field\": \"content\",
          \"query\": \"technology\"
        }
      }
    }" | jq '{next_cursor, has_more, total_matches, results: (.hits | length)}'
fi

# ============================================================================
# ERROR HANDLING EXAMPLES
# ============================================================================

echo -e "\n[10] Error - Missing required query"
curl -X POST "${API_BASE}/v1/articles/_search" \
  -H "Content-Type: application/json" \
  -d '{
    "size": 5,
    "query": {
      "match": {
        "field": "title"
      }
    }
  }' | jq '.'

echo -e "\n[11] Error - Invalid operator"
curl -X POST "${API_BASE}/v1/articles/_search" \
  -H "Content-Type: application/json" \
  -d '{
    "query": {
      "match": {
        "field": "title",
        "query": "test",
        "operator": "invalid"
      }
    }
  }' | jq '.'

echo -e "\n[12] Error - Unsupported language"
curl -X POST "${API_BASE}/v1/articles/_search" \
  -H "Content-Type: application/json" \
  -d '{
    "query": {
      "match": {
        "field": "title",
        "query": "test",
        "language": "french"
      }
    }
  }' | jq '.'

# ============================================================================
# ADVANCED USE CASES
# ============================================================================

echo -e "\n[13] Compare relevance - Same query, different field weights"
echo "Query with equal weights:"
curl -s -X POST "${API_BASE}/v1/articles/_search" \
  -H "Content-Type: application/json" \
  -d '{
    "size": 3,
    "query": {
      "multi_match": {
        "query": "artificial intelligence",
        "fields": ["title", "content"]
      }
    }
  }' | jq '[.hits[] | {title: .article.title, score: .score}]'

echo -e "\nSame query with title boosted 5x:"
curl -s -X POST "${API_BASE}/v1/articles/_search" \
  -H "Content-Type: application/json" \
  -d '{
    "size": 3,
    "query": {
      "multi_match": {
        "query": "artificial intelligence",
        "fields": ["title", "content"],
        "field_weights": {
          "title": 5.0,
          "content": 1.0
        }
      }
    }
  }' | jq '[.hits[] | {title: .article.title, score: .score}]'

echo -e "\n[14] AND vs OR comparison"
echo "OR operator (at least one term matches):"
curl -s -X POST "${API_BASE}/v1/articles/_search" \
  -H "Content-Type: application/json" \
  -d '{
    "size": 3,
    "query": {
      "match": {
        "field": "title",
        "query": "climate change",
        "operator": "or"
      }
    }
  }' | jq '.total_matches'

echo -e "\nAND operator (all terms must match):"
curl -s -X POST "${API_BASE}/v1/articles/_search" \
  -H "Content-Type: application/json" \
  -d '{
    "size": 3,
    "query": {
      "match": {
        "field": "title",
        "query": "climate change",
        "operator": "and"
      }
    }
  }' | jq '.total_matches'

echo -e "\n================================================"
echo "Examples completed!"
echo "================================================"

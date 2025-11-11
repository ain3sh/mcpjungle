# Search Feature Testing Guide

This guide provides a comprehensive recipe for testing the tool search functionality in MCPJungle.

## Prerequisites

- Go 1.21+ installed
- Python 3 installed (for mock server)
- `jq` installed (optional, for prettier JSON output)

## Testing Recipe

### Step 1: Build the Project

```bash
cd mcpjungle
go build -o mcpjungle main.go
```

Verify the build succeeded and the binary is executable.

### Step 2: Unit Tests

Run the search-specific unit tests:

```bash
# Test the search service
go test -v ./internal/service/search/...

# Test the search meta-tool
go test -v -run "TestSearchMetaTool|TestInitSearchMetaTool" ./internal/service/mcp/...
```

Expected: All tests should pass.

### Step 3: Integration Test with Mock MCP Server

Use the provided integration test script that:
1. Starts a mock MCP server with sample tools
2. Starts MCPJungle in development mode
3. Registers the mock server
4. Tests all search functionality

```bash
./scripts/test_search_integration.sh
```

## Integration Test Script

Save this as `scripts/test_search_integration.sh`:

```bash
#!/bin/bash

echo "=== MCPJungle Search Feature Integration Test ==="
echo

# Cleanup function
cleanup() {
    echo "Cleaning up..."
    pkill -f mcpjungle 2>/dev/null
    pkill -f "python.*mock_mcp_server" 2>/dev/null
    rm -f mock_mcp_server.py
    sleep 1
}

# Ensure cleanup on exit
trap cleanup EXIT

# Initial cleanup
cleanup

# Create a mock MCP server
cat > mock_mcp_server.py << 'EOF'
#!/usr/bin/env python3
from http.server import HTTPServer, BaseHTTPRequestHandler
import json

class MCPHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        content_length = int(self.headers['Content-Length'])
        body = self.rfile.read(content_length)
        request = json.loads(body)
        
        response = {
            "jsonrpc": "2.0",
            "id": request.get("id", 1)
        }
        
        if request.get("method") == "initialize":
            response["result"] = {
                "protocolVersion": "2024-11-05",
                "capabilities": {
                    "tools": {},
                    "prompts": {}
                },
                "serverInfo": {
                    "name": "mock-server",
                    "version": "1.0.0"
                }
            }
        elif request.get("method") == "tools/list":
            response["result"] = {
                "tools": [
                    {
                        "name": "read_file",
                        "description": "Read contents of a file from the filesystem",
                        "inputSchema": {
                            "type": "object",
                            "properties": {
                                "path": {"type": "string"}
                            },
                            "required": ["path"]
                        }
                    },
                    {
                        "name": "write_file", 
                        "description": "Write content to a file in the filesystem",
                        "inputSchema": {
                            "type": "object",
                            "properties": {
                                "path": {"type": "string"},
                                "content": {"type": "string"}
                            },
                            "required": ["path", "content"]
                        }
                    },
                    {
                        "name": "list_directory",
                        "description": "List files in a directory",
                        "inputSchema": {
                            "type": "object",
                            "properties": {
                                "path": {"type": "string"}
                            },
                            "required": ["path"]
                        }
                    }
                ]
            }
        elif request.get("method") == "prompts/list":
            response["result"] = {"prompts": []}
        else:
            response["error"] = {
                "code": -32601,
                "message": "Method not found"
            }
        
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(json.dumps(response).encode())
    
    def log_message(self, format, *args):
        pass  # Suppress logs

if __name__ == "__main__":
    server = HTTPServer(('localhost', 9000), MCPHandler)
    print("Mock MCP server ready on http://localhost:9000")
    server.serve_forever()
EOF

# Start mock MCP server
echo "Starting mock MCP server..."
python3 mock_mcp_server.py &
MOCK_PID=$!

# Wait for mock server
sleep 2
if ! kill -0 $MOCK_PID 2>/dev/null; then
    echo "❌ Failed to start mock MCP server"
    exit 1
fi
echo "✅ Mock MCP server started"

# Start MCPJungle server
echo "Starting MCPJungle server..."
SERVER_MODE=development ./mcpjungle start --port 8080 > server.log 2>&1 &
SERVER_PID=$!

# Wait for MCPJungle to be healthy
echo "Waiting for MCPJungle to be healthy..."
for i in {1..10}; do
    if curl -s http://localhost:8080/health | grep -q "ok"; then
        echo "✅ MCPJungle is healthy"
        break
    fi
    if [ $i -eq 10 ]; then
        echo "❌ MCPJungle failed to start"
        cat server.log
        exit 1
    fi
    sleep 1
done

# Register the mock server
echo
echo "Registering mock MCP server..."
OUTPUT=$(./mcpjungle register --name filesystem --url http://localhost:9000 --description "Mock filesystem server" 2>&1)
if echo "$OUTPUT" | grep -q "registered successfully"; then
    echo "✅ Mock server registered"
else
    echo "❌ Failed to register mock server: $OUTPUT"
    exit 1
fi

# Allow time for tools to be loaded
sleep 2

# Run test suite
echo
echo "=== Running Search Tests ==="
TESTS_PASSED=0
TESTS_FAILED=0

# Test 1: Search endpoint exists and responds
echo
echo "Test 1: Search endpoint accessibility"
RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" "http://localhost:8080/api/v0/tools/search?q=test")
HTTP_CODE=$(echo "$RESPONSE" | grep "HTTP_CODE:" | cut -d: -f2)
if [ "$HTTP_CODE" = "200" ]; then
    echo "✅ Search endpoint accessible (HTTP 200)"
    ((TESTS_PASSED++))
else
    echo "❌ Search endpoint returned HTTP $HTTP_CODE"
    ((TESTS_FAILED++))
fi

# Test 2: Search finds tools with 'file' keyword
echo
echo "Test 2: Search for 'file' keyword"
RESULT=$(curl -s "http://localhost:8080/api/v0/tools/search?q=file")
COUNT=$(echo "$RESULT" | grep -o '"count":[0-9]*' | cut -d: -f2)
if [ "$COUNT" -ge 2 ]; then
    echo "✅ Found $COUNT tools matching 'file'"
    ((TESTS_PASSED++))
else
    echo "❌ Expected at least 2 results for 'file', got $COUNT"
    echo "Response: $RESULT"
    ((TESTS_FAILED++))
fi

# Test 3: Search with specific keyword
echo
echo "Test 3: Search for 'read' keyword"
RESULT=$(curl -s "http://localhost:8080/api/v0/tools/search?q=read")
if echo "$RESULT" | grep -q "read_file"; then
    echo "✅ Found 'read_file' tool"
    ((TESTS_PASSED++))
else
    echo "❌ 'read_file' not found in search results"
    echo "Response: $RESULT"
    ((TESTS_FAILED++))
fi

# Test 4: Server filtering
echo
echo "Test 4: Filter by server name"
RESULT=$(curl -s "http://localhost:8080/api/v0/tools/search?q=file&server[]=filesystem")
if echo "$RESULT" | grep -q '"server_name":"filesystem"'; then
    echo "✅ Server filtering works"
    ((TESTS_PASSED++))
else
    echo "❌ Server filtering failed"
    echo "Response: $RESULT"
    ((TESTS_FAILED++))
fi

# Test 5: Max results limit
echo
echo "Test 5: Max results limit"
RESULT=$(curl -s "http://localhost:8080/api/v0/tools/search?q=file&max_results=1")
COUNT=$(echo "$RESULT" | grep -o '"tool_name"' | wc -l)
if [ "$COUNT" -eq 1 ]; then
    echo "✅ Max results limit works (returned 1 result)"
    ((TESTS_PASSED++))
else
    echo "❌ Max results limit failed (returned $COUNT results)"
    echo "Response: $RESULT"
    ((TESTS_FAILED++))
fi

# Test 6: Search scoring
echo
echo "Test 6: Search result scoring"
RESULT=$(curl -s "http://localhost:8080/api/v0/tools/search?q=file")
FIRST_SCORE=$(echo "$RESULT" | grep -o '"score":[0-9.]*' | head -1 | cut -d: -f2 | cut -d. -f1)
if [ "$FIRST_SCORE" -ge 5 ]; then
    echo "✅ Search scoring works (first result score: $FIRST_SCORE)"
    ((TESTS_PASSED++))
else
    echo "❌ Unexpected scoring (first result score: $FIRST_SCORE)"
    ((TESTS_FAILED++))
fi

# Test 7: Empty query handling
echo
echo "Test 7: Empty query handling"
RESPONSE=$(curl -s -w "\nHTTP_CODE:%{http_code}" "http://localhost:8080/api/v0/tools/search")
HTTP_CODE=$(echo "$RESPONSE" | grep "HTTP_CODE:" | cut -d: -f2)
if [ "$HTTP_CODE" = "400" ]; then
    echo "✅ Empty query correctly returns 400"
    ((TESTS_PASSED++))
else
    echo "❌ Empty query returned HTTP $HTTP_CODE (expected 400)"
    ((TESTS_FAILED++))
fi

# Test Summary
echo
echo "=== Test Summary ==="
echo "Tests Passed: $TESTS_PASSED"
echo "Tests Failed: $TESTS_FAILED"

if [ $TESTS_FAILED -eq 0 ]; then
    echo "✅ All tests passed!"
    exit 0
else
    echo "❌ Some tests failed"
    exit 1
fi
```

## Expected Test Results

When all tests pass, you should see:

```
=== Test Summary ===
Tests Passed: 7
Tests Failed: 0
✅ All tests passed!
```

## Manual Testing

For manual testing, you can use these curl commands after starting the server and registering an MCP server:

```bash
# Basic search
curl -s "http://localhost:8080/api/v0/tools/search?q=file" | jq '.'

# Search with server filter
curl -s "http://localhost:8080/api/v0/tools/search?q=file&server[]=filesystem" | jq '.'

# Search with max results
curl -s "http://localhost:8080/api/v0/tools/search?q=file&max_results=2" | jq '.'

# Search with only enabled tools
curl -s "http://localhost:8080/api/v0/tools/search?q=file&only_enabled=true" | jq '.'
```

## Troubleshooting

### Server won't start
- Check if port 8080 is already in use: `lsof -i :8080`
- Check server logs: `cat server.log`

### Mock server issues
- Ensure Python 3 is installed: `python3 --version`
- Check if port 9000 is available: `lsof -i :9000`

### Search returns no results
- Verify MCP server is registered: `./mcpjungle list servers`
- Check if tools are loaded: `./mcpjungle list tools`
- Try a broader search term

## CI/CD Integration

To integrate this test in CI/CD:

```yaml
# Example GitHub Actions workflow
- name: Build MCPJungle
  run: go build -o mcpjungle main.go

- name: Run unit tests
  run: |
    go test -v ./internal/service/search/...
    go test -v -run "TestSearchMetaTool" ./internal/service/mcp/...

- name: Run integration tests
  run: |
    chmod +x scripts/test_search_integration.sh
    ./scripts/test_search_integration.sh
```

## Performance Testing

For performance testing with many tools:

1. Modify the mock server to return 100+ tools
2. Test search response times
3. Verify pagination works correctly

Expected performance:
- Search with 100 tools: < 100ms
- Search with 1000 tools: < 500ms

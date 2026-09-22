// Minimal real stdio MCP child: initialization, discovery, and a credential check.
import { createInterface } from 'node:readline';
for await (const line of createInterface({ input: process.stdin })) {
  const message = JSON.parse(line);
  if (message.id === undefined) continue;
  let result = {};
  if (message.method === 'initialize') result = {
    protocolVersion: message.params.protocolVersion,
    capabilities: { tools: {} }, serverInfo: { name: 'airun-acceptance', version: '1.0.0' }
  };
  if (message.method === 'tools/list') result = { tools: [{ name: 'credential_probe', description: 'Verify this MCP child received its synthetic credential', inputSchema: { type: 'object', properties: {} } }] };
  if (message.method === 'tools/call') result = {
    content: [{ type: 'text', text: process.env.TOKEN === `synthetic-mcp-${process.argv[2]}` ? `AIRUN_MCP_${process.argv[2]}_CREDENTIAL_OK` : 'AIRUN_MCP_CREDENTIAL_WRONG' }]
  };
  process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: message.id, result }) + '\n');
}

// usage: node mkconfig.mjs OUT.json WORKDIR SCRIPTSDIR MCP_NATS NATS_URL NODE  -> Computer -config file
import { writeFileSync } from 'node:fs';
const [out, work, scripts, mcp, url, node] = process.argv.slice(2);
const server = mcp.endsWith('.js') || mcp.endsWith('.mjs') ? { command: node, args: [mcp] } : { command: mcp };
server.env = { NATS_URL: url };
writeFileSync(out, JSON.stringify({ mounts: [`${work}:/work`, `${scripts}:/scripts:ro`], mcpServers: { nats: server } }, null, 1));

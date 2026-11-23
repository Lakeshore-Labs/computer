const fs = require('node:fs')
const { Client } = require('@modelcontextprotocol/sdk/client/index.js')
const { StdioClientTransport } = require('@modelcontextprotocol/sdk/client/stdio.js')
async function main() {
  const [binary, work, recipe, runtime] = process.argv.slice(2)
  const transport = new StdioClientTransport({command: binary, args: [
    '-mount', work + ':/work', '-mount', recipe + ':/recipe:ro',
    '-python', runtime + '/python.wasm', '-python-lib', runtime + '/lib', 'mcp'
  ]})
  const client = new Client({name: 'festival-box-office', version:'1.0.0'})
  await client.connect(transport)
  try {
    const input = fs.readFileSync(recipe + '/program.json', 'utf8')
    const result = await client.callTool({name: 'write', arguments: {path:'program.json', content: input}})
    if (result.isError) throw new Error(JSON.stringify(result.content))
    const published = await client.callTool({name:'bash', arguments:{command:'python3 /recipe/publish.py original'}})
    if (published.isError) throw new Error(JSON.stringify(published.content))
    console.log(JSON.stringify({client: 'TypeScript SDK', server:client.getServerVersion().name,
      operations:['write program.json','publish original calendar and lobby program'], result:published.content}))
  } finally {
    await client.close()
  }
}
main().catch(e=>{console.error(e);process.exitCode=1})

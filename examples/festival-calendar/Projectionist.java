import io.modelcontextprotocol.client.McpClient;
import io.modelcontextprotocol.client.McpSyncClient;
import io.modelcontextprotocol.client.transport.ServerParameters;
import io.modelcontextprotocol.client.transport.StdioClientTransport;
import io.modelcontextprotocol.json.jackson2.JacksonMcpJsonMapperSupplier;
import io.modelcontextprotocol.spec.McpSchema;
import java.time.Duration;
import java.util.Map;

class Projectionist {
    static McpSchema.CallToolResult call(McpSyncClient client, String name, Map<String, Object> args) {
        var result = client.callTool(new McpSchema.CallToolRequest(name, args));
        if (Boolean.TRUE.equals(result.isError())) {
            throw new IllegalStateException(result.content().toString());
        }
        return result;
    }

    public static void main(String[] args) throws Exception {
        var parameters = ServerParameters.builder(args[0]).args(
            "-mount", args[1] + ":/work", "-mount", args[2] + ":/recipe:ro",
            "-python", args[3] + "/python.wasm", "-python-lib", args[3] + "/lib", "mcp").build();
        var mapper = new JacksonMcpJsonMapperSupplier().get();
        var transport = new StdioClientTransport(parameters, mapper);
        try (var client = McpClient.sync(transport).requestTimeout(Duration.ofSeconds(60)).build()) {
            var init = client.initialize();
            call(client, "edit", Map.of("path", "program.json", "oldText", "\"minutes\":25", "newText", "\"minutes\":40"));
            var publish = call(client, "bash", Map.of("command", "python3 /recipe/publish.py amended"));
            System.out.println(mapper.writeValueAsString(Map.of("client", "Java SDK", "server", init.serverInfo().name(),
                "operations", new String[]{"edit first film duration", "publish amended calendar and lobby program"},
                "result", publish.content())));
        }
    }
}

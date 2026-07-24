export function buildWorkseedMcpEndpoint(port: string) {
  return `http://127.0.0.1${port ? `:${port}` : ''}/mcp`
}

export function formatWorkseedMcpConfig(port: string) {
  return JSON.stringify({
    mcpServers: {
      workseed: {
        url: buildWorkseedMcpEndpoint(port),
      },
    },
  }, null, 2)
}

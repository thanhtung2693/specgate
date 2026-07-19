import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'node:path'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const specgateTarget = env.SPECGATE_PROXY_TARGET
  const docRegistryTarget = env.DOC_REGISTRY_PROXY_TARGET || 'http://127.0.0.1:8080'
  const agentsTarget = env.LANGGRAPH_PROXY_TARGET || 'http://127.0.0.1:2024'

  return {
    plugins: [react(), tailwindcss()],
    server: {
      proxy: {
        '/api/doc-registry': {
          target: specgateTarget || docRegistryTarget,
          changeOrigin: true,
          ...(specgateTarget
            ? {}
            : { rewrite: (url: string) => url.replace(/^\/api\/doc-registry/, '') }),
        },
        '/api/agents': {
          target: specgateTarget || agentsTarget,
          changeOrigin: true,
          ...(specgateTarget
            ? {}
            : { rewrite: (url: string) => url.replace(/^\/api\/agents/, '') }),
        },
      },
    },
    build: {
      chunkSizeWarningLimit: 3200,
      rollupOptions: {
        output: {
          manualChunks(id) {
            if (id.includes('@langchain/langgraph-sdk')) {
              return 'langgraph-sdk'
            }
            if (id.includes('@assistant-ui/react-langgraph')) {
              return 'assistant-langgraph'
            }
            if (id.includes('node_modules/mermaid')) {
              return 'mermaid'
            }
          },
        },
      },
    },
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
  }
})

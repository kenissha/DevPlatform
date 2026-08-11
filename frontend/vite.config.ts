import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    // In production the backend serves the built frontend and the API
    // from the same origin (see the design doc's embed-into-binary plan),
    // so there's no CORS setup anywhere. `npm run dev` proxies API calls
    // to the Go backend instead so that dev mode matches the same
    // same-origin assumption without needing one.
    proxy: {
      '/api': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
    },
  },
})

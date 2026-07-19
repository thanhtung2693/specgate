import { Navigate, Route, Routes } from "react-router-dom"

import { AppShell } from "@/components/layout/app-shell"
import { QueryProvider } from "@/providers/query-provider"
import { ThemeProvider } from "@/providers/theme-provider"

function App() {
  return (
    <QueryProvider>
      <ThemeProvider>
        <Routes>
          <Route element={<AppShell />}>
            <Route index element={<Navigate to="/work" replace />} />
            <Route path="/:section" element={null} />
            <Route path="/:section/:itemKey" element={null} />
          </Route>
        </Routes>
      </ThemeProvider>
    </QueryProvider>
  )
}

export default App

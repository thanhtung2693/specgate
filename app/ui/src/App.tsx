import { Navigate, Route, Routes } from "react-router-dom"

import { AppShell } from "@/components/layout/app-shell"
import { ThemeProvider } from "@/providers/theme-provider"

function App() {
  return (
    <ThemeProvider>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<Navigate to="/work" replace />} />
          <Route path="/:section" element={null} />
          <Route path="/:section/:itemKey" element={null} />
        </Route>
      </Routes>
    </ThemeProvider>
  )
}

export default App

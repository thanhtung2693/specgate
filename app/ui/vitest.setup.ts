import "@testing-library/jest-dom/vitest"
import { vi } from "vitest"

vi.stubEnv("VITE_DOC_REGISTRY_URL", "")
vi.stubEnv("VITE_LANGGRAPH_API_URL", "")

class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}

Object.defineProperty(window, "ResizeObserver", {
  writable: true,
  configurable: true,
  value: ResizeObserverMock,
})

Object.defineProperty(HTMLElement.prototype, "scrollTo", {
  writable: true,
  configurable: true,
  value: () => {},
})

Object.defineProperty(URL, "createObjectURL", {
  writable: true,
  configurable: true,
  value: () => "blob:test-object-url",
})

Object.defineProperty(URL, "revokeObjectURL", {
  writable: true,
  configurable: true,
  value: () => {},
})

Object.defineProperty(window, "matchMedia", {
  writable: true,
  configurable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
})

/// <reference types="vite/client" />
import type { TestingLibraryMatchers } from "@testing-library/jest-dom/matchers"

// jest-dom 7 still augments Vitest's pre-v5 assertion shape.
declare module "vitest" {
  interface Matchers<R extends void | Promise<void> = void | Promise<void>, T = unknown>
    extends TestingLibraryMatchers<T, R> {}
}

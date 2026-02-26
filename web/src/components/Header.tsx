import { Link } from "@tanstack/react-router";
import { Github } from "lucide-react";

export function Header() {
  return (
    <header className="sticky top-0 z-50 w-full border-b border-border bg-background/95 backdrop-blur supports-backdrop-filter:bg-background/60">
      <div className="container flex h-16 items-center justify-between">
        <div className="flex items-center gap-8">
          <Link to="/" className="flex items-center gap-2">
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 200 200"
              className="h-10.5 w-10.5"
            >
              <path
                d="m106 3.67 25.69 15.24c3.11 1.88 1.95 4.88-0.5 6.32l-26.23 15.91c-4.08 2.56-7.12 2.24-10.23 0.08l-25.73-15.88c-2.66-1.59-2.9-4.43-0.32-6.13l26.02-15.69c3.44-2.11 7.24-2.43 11.3 0.15z"
                fill="currentColor"
              />
              <path
                d="m146.4 28.73 27.47 16.89c3.36 2.09 2.36 4.73 0.36 6l-27.19 16.99c-3.87 2.43-6.78 1.58-10.14-0.27l-26.83-16.83c-3.08-1.9-3.45-4.2-0.3-6.29l27.27-16.45c3.73-2.28 6.29-2.01 9.36-0.04z"
                fill="currentColor"
              />
              <path
                d="m62.89 28.21 27.05 16.74c3.53 2.16 2.77 4.88-0.08 6.71l-27.01 16.7c-3.66 2.35-7.57 1.25-10.64-0.64l-26.48-16.21c-2.61-1.52-2.94-4.16 0.08-6.09l27.16-16.61c3.98-2.44 6.64-2.32 9.92-0.6z"
                fill="currentColor"
              />
              <path
                d="m104.1 54.47 27.08 16.84c3.96 2.5 3.74 5.02 0.94 6.91l-27.98 16.7c-3.51 2.2-6.17 1.8-9.24-0.16l-26.83-16.74c-2.74-1.65-2.94-4.09 0.4-6.22l26.87-17.28c2.89-1.81 6.01-1.61 8.76-0.05z"
                fill="currentColor"
              />
              <path
                d="m178.8 55.28-28.58 17.71c-3.24 2.1-4.12 4.95-4.4 8.02l-0.6 31.1c-0.12 4.61 2 4.77 5.07 2.59l26.44-16.48c2.56-1.52 3.99-4.97 4.23-6.49l0.72-33.57c0.08-2.24-1.16-3.32-2.88-2.88z"
                fill="currentColor"
              />
              <path
                d="m21.29 55.12 28.39 17.44c3.24 1.97 4.24 4.82 4.24 7.89l0.68 31.46c0.08 4.21-1.88 4.81-5.32 2.65l-25.47-15.69c-3.08-1.83-5.27-3.99-5.39-8.88l-0.68-31.31c-0.04-2.24 1.12-3.84 3.55-3.56z"
                fill="currentColor"
              />
              <path
                d="m134.9 83.01-27.52 16.3c-3.28 1.97-5.08 5.29-5.12 9.16l-0.16 31.34c-0.04 2.81 2.12 3.41 4.56 1.72l27.24-16.26c2.96-1.83 4.93-5.2 4.93-8.67l0.52-30.82c0.08-3.37-1.85-4.13-4.45-2.77z"
                fill="currentColor"
              />
              <path
                d="m64.33 82.49 28.18 16.42c3.52 2.05 4.8 5.73 4.72 9.2l-0.08 31.82c0 3.15-2.04 3.59-5.24 1.59l-25.47-16.05c-3.35-2.04-5.91-4.76-6.03-9.65l-0.44-31.06c-0.04-2.63 1.56-3.63 4.36-2.27z"
                fill="currentColor"
              />
              <path
                d="m177.9 104.9-28.1 17.39c-3.25 1.97-4.73 5.33-4.93 8.4l-0.68 33.69c-0.08 3.24 2.4 3.64 4 2.64l27.51-17.34c2.96-1.83 4.24-5.19 4.36-8.08l0.75-32.95c0.08-2.84-1.19-4.23-2.91-3.75z"
                fill="currentColor"
              />
              <path
                d="m21.93 104.7 28.31 17.31c3.24 1.96 4.52 5.09 4.64 7.81l0.56 33.76c0.08 3.56-2.28 4.16-4.56 2.72l-26.55-16.06c-3.24-1.88-5.19-4.04-5.31-8.15l-0.4-34.24c-0.04-2.23 0.96-3.75 3.31-3.15z"
                fill="currentColor"
              />
              <path
                d="m135.1 132.6-27.92 17.34c-3.24 1.96-5 5.32-5 8.39l-0.16 32.06c-0.04 3.87 2.88 4.03 5.24 2.36l26.44-17.79c2.96-2.16 4.12-5.52 4.12-8.4l0.88-31.8c0.08-2.84-1.08-3.56-3.6-2.16z"
                fill="currentColor"
              />
              <path
                d="m64.81 132.3 27.58 17.14c3.52 2.24 4.72 5.92 4.64 8.99l0.08 32.3c0 3.43-2.76 3.87-5.68 1.6l-25.11-16.79c-2.95-1.84-4.95-4.36-5.07-9.24l-0.72-31.36c-0.04-2.64 1.52-3.88 4.28-2.64z"
                fill="currentColor"
              />
            </svg>
            <span className="font-bold text-lg tracking-tight">Microvault</span>
          </Link>

          <nav className="hidden md:flex items-center gap-6">
            <Link
              to="/our-approach"
              className="text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
            >
              Our Approach
            </Link>
          </nav>
        </div>

        <div className="flex items-center gap-4">
          <a
            href="https://github.com/Shamba-Records-Limited/microvault/"
            target="_blank"
            rel="noopener noreferrer"
            className="h-9 w-9 rounded-xl bg-foreground flex items-center justify-center hover:opacity-80 transition-opacity"
          >
            <Github className="h-5 w-5 text-background" />
          </a>
        </div>
      </div>
    </header>
  );
}

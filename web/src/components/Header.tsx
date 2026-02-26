import { Link } from "@tanstack/react-router";
import { Github } from "lucide-react";

export function Header() {
  return (
    <header className="sticky top-0 z-50 w-full border-b border-border bg-background/95 backdrop-blur supports-backdrop-filter:bg-background/60">
      <div className="container flex h-16 items-center justify-between">
        <div className="flex items-center gap-8">
          <Link to="/" className="flex items-center gap-2">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" fill="none" className="h-10.5 w-10.5">
              <path d="M 137.49,8.42 C 129.62,4.13 123.19,4.64 116.75,8.06 L 28.11,53.78 C 23.72,56.27 22.25,57.98 24.14,59.21 L 113.12,104.34 C 123.28,109.13 133.39,108.13 142.49,103.79 L 231.47,59.11 C 233.63,58.01 232.64,56.22 227.62,53.59 L 137.49,8.42 Z M 193.78,54.22 L 129.51,20.03 C 127.67,19.04 126.76,19.21 125.73,19.72 L 61.63,53.73 C 58.24,55.56 58.56,59.34 60.94,60.52 L 124.28,93.49 C 127.06,94.94 128.84,94.21 130.63,93.22 L 193.83,60.83 C 197.46,58.91 197.01,55.97 193.78,54.22 Z M 128.71,28.71 L 174.87,53.24 C 175.91,53.78 175.82,54.41 174.11,54.41 H 140.28 C 138.66,54.41 137.99,54.68 138.04,56.31 L 138.13,59.34 C 138.18,60.52 138.63,60.92 140.33,60.92 H 174.73 C 176.11,60.92 175.74,61.46 174.96,61.87 L 131.62,84.43 V 55.7 C 131.62,54.22 130.76,53.68 129.25,53.68 H 126.47 C 124.68,53.68 123.59,53.91 123.59,55.88 V 84.7 L 81.22,62.42 C 80.04,61.78 79.63,60.92 81.42,60.92 H 114.32 C 116.58,60.92 117.03,60.61 116.98,59.11 L 116.89,55.79 C 116.84,54.64 116.56,53.96 114.73,53.96 H 81.73 C 80.08,53.96 79.72,53.64 80.99,52.82 L 126.69,28.53 C 127.51,28.08 127.83,28.26 128.71,28.71 Z" fill="currentColor"/>
              <path d="M 235.64,66.25 L 148.21,112.61 C 138.59,117.72 131.53,125.92 131.53,138.84 V 247.13 C 131.53,250.12 133.69,250.57 136.12,249.12 L 228.12,200.69 C 235.9,196.62 238.16,191.92 238.16,184.82 V 70.08 C 238.16,67.26 237.17,65.47 235.64,66.25 Z M 220.56,98.73 C 220.29,96.11 217.46,95.38 215.03,96.74 L 152.2,128.33 C 149.19,129.87 148.74,131.09 148.74,134.46 V 213.29 C 148.74,217.27 151.47,218.86 155.19,216.72 L 217.1,185.32 C 220.06,183.78 220.51,182.42 220.51,178.78 L 220.65,100.14 C 220.65,99.46 220.65,99.01 220.56,98.73 Z" fill="currentColor"/>
              <path d="M 19.61,66.07 C 17.82,66.34 16.73,68.17 16.73,73.51 V 184.91 C 16.73,192.66 19.16,197.41 26.81,201.3 L 118.81,249.73 C 121.72,250.96 123.19,250.01 123.19,247.48 V 137.18 C 123.19,125.74 117.21,116.75 107.92,111.72 L 21.08,66.56 C 20.48,66.25 20.03,66.03 19.61,66.07 Z M 37.78,96.06 C 36.22,96.78 34.71,97.83 34.71,101.2 V 179.14 C 34.71,182.24 35.21,183.95 37.78,185.27 L 101.21,217.99 C 104.66,219.82 106.36,217.05 106.36,214.33 V 134.01 C 106.36,131.06 105.18,129.52 103.11,128.42 L 40.19,96.28 C 39.11,95.7 38.38,95.79 37.78,96.06 Z" fill="currentColor"/>
              <path d="M 42.84,115.6 L 62.51,147.46 C 63.28,148.73 63.78,148.37 64.51,147.82 L 67.91,145.38 C 68.99,144.56 68.54,143.88 67.91,142.74 L 50.01,111.2 L 94.34,133.89 L 42.84,172.46 V 115.6 Z" fill="currentColor"/>
              <path d="M 47.36,180.28 L 98.29,141.49 V 206.91 L 47.36,180.28 Z" fill="currentColor"/>
              <path d="M 161.66,133.8 L 204.78,111.02 L 186.92,142.74 C 186.29,143.88 186.29,144.61 187.33,145.33 L 191.18,148.01 C 192.13,148.64 192.81,147.69 193.31,146.78 L 212.28,115.24 V 172.32 L 161.66,133.8 Z" fill="currentColor"/>
              <path d="M 156.73,140.81 L 208.23,180.28 L 156.73,206.91 V 140.81 Z" fill="currentColor"/>
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

import { Link } from "@tanstack/react-router";
import { Github } from "lucide-react";
import logo from "@/assets/microvault-logo.png";

export function Header() {
  return (
    <header className="sticky top-0 z-50 w-full border-b border-border bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="container flex h-16 items-center justify-between">
        <div className="flex items-center gap-8">
          <Link to="/" className="flex items-center gap-2">
            <img
              src={logo}
              alt="Microvault logo"
              className="h-14 w-14 object-contain"
            />
            <span className="font-bold text-lg tracking-tight">Microvault</span>
          </Link>

          <nav className="hidden md:flex items-center gap-6">
            <Link
              to="/use-case"
              className="text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
            >
              Use Case
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

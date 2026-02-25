import { Link } from "@tanstack/react-router";
import { Separator } from "@/components/ui/separator";

export default function NotFoundPage() {
  return (
    <main className="container py-16 max-w-4xl">
      <section className="flex flex-col items-center text-center py-24">
        <p className="text-xs font-mono uppercase tracking-widest text-muted-foreground mb-4">
          404
        </p>
        <h1 className="text-4xl md:text-5xl font-bold leading-tight mb-4">
          Page Not Found
        </h1>
        <p className="text-lg text-muted-foreground leading-relaxed max-w-md mb-8">
          The page you&apos;re looking for doesn&apos;t exist or has been moved.
        </p>

        <Separator className="mb-8 max-w-xs" />

        <div className="flex items-center gap-4">
          <Link
            to="/"
            className="inline-flex h-10 items-center justify-center rounded-lg bg-foreground px-6 text-sm font-medium text-background hover:opacity-80 transition-opacity"
          >
            Back to Pools
          </Link>
          <Link
            to="/our-approach"
            className="inline-flex h-10 items-center justify-center rounded-lg border border-border px-6 text-sm font-medium text-foreground hover:bg-muted/30 transition-colors"
          >
            Our Approach
          </Link>
        </div>
      </section>
    </main>
  );
}

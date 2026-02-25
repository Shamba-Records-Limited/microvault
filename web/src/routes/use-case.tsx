import { Separator } from "@/components/ui/separator";
import {
  Sprout,
  Globe,
  TrendingUp,
  ShieldCheck,
  Zap,
  Users,
} from "lucide-react";

const stats = [
  { value: "2.5B", label: "Africa's population by 2050" },
  { value: "$170B", label: "Farmer lending gap in Africa" },
  { value: "6\u201310%", label: "Farmers with access to formal credit" },
  { value: "60%", label: "Population employed in agriculture" },
];

const pillars = [
  {
    icon: ShieldCheck,
    title: "Farmer Credential Verification",
    description:
      "Soil data, crop yields, market records, and weather resilience are aggregated into tamper-proof verifiable credentials \u2014 giving farmers a financial identity based on productivity, not collateral.",
  },
  {
    icon: Zap,
    title: "AI-Powered Credit Scoring",
    description:
      "Shamba Records\u2019 AI analyses real farm productivity, market potential, and climate resilience to generate credit scores that reflect a farmer\u2019s true financial standing.",
  },
  {
    icon: Globe,
    title: "On-Chain Lending Vaults",
    description:
      "Built on Stellar\u2019s Soroban smart contract platform using the SEP-56 Vault standard, every loan is transparent, auditable, and accessible \u2014 no intermediaries, no hidden fees.",
  },
  {
    icon: Users,
    title: "Verifiable Digital Identity",
    description:
      "Farmer credentials plug directly into DeFi platforms, banks, and microfinance institutions, enabling last-mile financing at a scale traditional systems have never reached.",
  },
];

export default function UseCasePage() {
  return (
    <main className="container py-16 max-w-4xl">
      {/* Hero */}
      <section className="mb-16">
        <p className="text-xs font-mono uppercase tracking-widest text-muted-foreground mb-4">
          Use Case
        </p>
        <h1 className="text-4xl md:text-5xl font-bold leading-tight mb-6">
          Bridging the{" "}
          <span className="underline decoration-2 underline-offset-4">
            $170 Billion
          </span>{" "}
          Lending Gap in Agriculture
        </h1>
        <p className="text-lg text-muted-foreground leading-relaxed max-w-2xl">
          Smallholder farmers feed the world. Yet the world&apos;s financial
          systems have left them behind. Microvault changes that by turning farm
          data into financial identity, and financial identity into credit.
        </p>
      </section>

      <Separator className="mb-16" />

      {/* Stats */}
      <section className="mb-16">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-8">
          {stats.map((stat) => (
            <div key={stat.label}>
              <p className="text-3xl font-bold tracking-tight mb-1">
                {stat.value}
              </p>
              <p className="text-xs text-muted-foreground leading-snug">
                {stat.label}
              </p>
            </div>
          ))}
        </div>
      </section>

      <Separator className="mb-16" />

      {/* The Problem */}
      <section className="mb-16">
        <div className="flex items-start gap-4 mb-6">
          <div className="h-8 w-8 rounded bg-foreground flex items-center justify-center shrink-0 mt-1">
            <Sprout className="h-4 w-4 text-background" />
          </div>
          <div>
            <h2 className="text-2xl font-bold mb-4">The Problem</h2>
            <div className="space-y-4 text-muted-foreground leading-relaxed">
              <p>
                Despite employing over 60% of Africa&apos;s workforce and
                anchoring the continent&apos;s food security, smallholder
                farmers are largely invisible to traditional financial
                institutions. They lack documented credit histories, collateral,
                and verifiable credentials &mdash; the very things banks
                require.
              </p>
              <p>
                With Africa&apos;s population projected to grow from 1.55
                billion today to 2.5 billion by 2050, the pressure on
                agriculture and food systems has never been more urgent. Yet
                only 6&ndash;10% of farmers have access to formal credit. The
                result: a{" "}
                <strong className="text-foreground">
                  $170 billion annual lending gap
                </strong>{" "}
                that stunts productivity, depresses incomes, and deepens food
                insecurity.
              </p>
            </div>
          </div>
        </div>
      </section>

      {/* The Solution */}
      <section className="mb-16">
        <div className="flex items-start gap-4 mb-8">
          <div className="h-8 w-8 rounded bg-foreground flex items-center justify-center shrink-0 mt-1">
            <TrendingUp className="h-4 w-4 text-background" />
          </div>
          <div className="w-full">
            <h2 className="text-2xl font-bold mb-4">The Solution</h2>
            <p className="text-muted-foreground leading-relaxed mb-8">
              Microvault is a micro-lending engine built by Shamba Records for
              emerging markets. It verifies farmer credentials using AI-powered
              data aggregation and issues verifiable digital identities that
              plug into credit scoring engines &mdash; giving farmers a
              financial footprint based not on what they own, but on what they
              produce.
            </p>
            <div className="grid md:grid-cols-2 gap-6">
              {pillars.map((pillar) => (
                <div
                  key={pillar.title}
                  className="border border-border rounded-lg p-5 hover:bg-muted/30 transition-colors"
                >
                  <div className="flex items-center gap-3 mb-3">
                    <pillar.icon className="h-4 w-4 text-foreground" />
                    <h3 className="font-semibold text-sm">{pillar.title}</h3>
                  </div>
                  <p className="text-sm text-muted-foreground leading-relaxed">
                    {pillar.description}
                  </p>
                </div>
              ))}
            </div>
          </div>
        </div>
      </section>

      <Separator className="mb-16" />

      {/* Impact */}
      <section className="mb-16">
        <h2 className="text-2xl font-bold mb-6">
          The Impact We&apos;re Building Toward
        </h2>
        <div className="grid md:grid-cols-3 gap-6">
          <div className="border-l-2 border-foreground pl-4">
            <p className="text-2xl font-bold mb-1">50,000</p>
            <p className="text-sm text-muted-foreground">
              Active Shamba Records users on the platform today
            </p>
          </div>
          <div className="border-l-2 border-foreground pl-4">
            <p className="text-2xl font-bold mb-1">5M+</p>
            <p className="text-sm text-muted-foreground">
              Farmers we aim to serve by 2030
            </p>
          </div>
          <div className="border-l-2 border-foreground pl-4">
            <p className="text-2xl font-bold mb-1">$100M</p>
            <p className="text-sm text-muted-foreground">
              In new credit projected within the next three years
            </p>
          </div>
        </div>
      </section>

      <Separator className="mb-16" />

      {/* Why Microvault */}
      <section className="mb-16">
        <h2 className="text-2xl font-bold mb-4">Why Microvault Is Different</h2>
        <p className="text-muted-foreground leading-relaxed max-w-2xl">
          Traditional finance asks farmers to prove they are creditworthy using
          documents and collateral they don&apos;t have. Microvault flips the
          model &mdash; using blockchain transparency, AI intelligence, and
          last-mile accessibility to build trust from the ground up. Through
          Stellar&apos;s Soroban smart contracts and the SEP-56 Vault standard,
          every loan is trustless, auditable, and scalable to reach millions of
          farmers at once.
        </p>
      </section>
    </main>
  );
}

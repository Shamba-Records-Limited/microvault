/**
 * Static marketing page describing Microvault's approach, partners, and use case.
 * @module routes/our-approach
 */
import {
  Globe,
  ShieldCheck,
  Zap,
  Users,
  ExternalLink,
} from "lucide-react";
import stellarLogo from "@/assets/stellar-logo.png";
import atLogo from "@/assets/at.png";
import yellowCardLogo from "@/assets/yellowcard-logo.avif";
import moneygramLogo from "@/assets/moneygram-logo.jpg";

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

/** Static "Our Approach" page — no data fetching, content is hard-coded. */
export default function UseCasePage() {
  return (
    <main className="container pt-24 pb-20 max-w-4xl">
      {/* Hero */}
      <section className="mb-24">
        <p className="text-[10px] font-mono uppercase tracking-[0.2em] font-bold text-muted-foreground mb-4">
          Our Approach
        </p>
        <h1 className="text-4xl md:text-5xl font-extrabold leading-tight tracking-tighter mb-6 text-foreground">
          Bridging the{" "}
          <span className="underline decoration-3 underline-offset-4">
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

      {/* Stats */}
      <section className="mb-20">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-8">
          {stats.map((stat) => (
            <div key={stat.label}>
              <p className="text-3xl font-bold tracking-tight tabular-nums mb-1">
                {stat.value}
              </p>
              <p className="text-sm text-muted-foreground leading-snug">
                {stat.label}
              </p>
            </div>
          ))}
        </div>
      </section>

      {/* The Problem */}
      <section className="mb-20">
        <h2 className="text-2xl font-bold leading-snug tracking-tight mb-4">The Problem</h2>
        <div className="space-y-4 text-muted-foreground leading-relaxed max-w-prose">
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
      </section>

      {/* The Solution */}
      <section className="mb-24">
        <h2 className="text-2xl font-bold leading-snug tracking-tight mb-4">The Solution</h2>
        <p className="text-muted-foreground leading-relaxed mb-8 max-w-prose">
          Microvault is a micro-lending engine built by Shamba Records for
          emerging markets. It verifies farmer credentials using AI-powered
          data aggregation and issues verifiable digital identities that
          plug into credit scoring engines &mdash; giving farmers a
          financial footprint based not on what they own, but on what they
          produce.
        </p>
        <div className="space-y-6 max-w-prose">
          {pillars.map((pillar) => (
            <div key={pillar.title}>
              <div className="flex items-center gap-2 mb-1.5">
                <pillar.icon className="h-4 w-4 text-foreground shrink-0" />
                <h3 className="font-semibold text-base text-foreground">{pillar.title}</h3>
              </div>
              <p className="text-sm text-muted-foreground leading-relaxed">
                {pillar.description}
              </p>
            </div>
          ))}
        </div>
      </section>

      {/* Impact */}
      <section className="mb-14">
        <h2 className="text-2xl font-bold leading-snug tracking-tight mb-4">
          The Impact We&apos;re Building Toward
        </h2>
        <div className="grid md:grid-cols-3 gap-x-8 gap-y-4">
          <div className="border-l border-foreground pl-3">
            <p className="text-2xl font-bold tabular-nums mb-0.5">50,000</p>
            <p className="text-xs text-muted-foreground leading-relaxed">
              Active Shamba Records users on the platform today
            </p>
          </div>
          <div className="border-l border-foreground pl-3">
            <p className="text-2xl font-bold tabular-nums mb-0.5">5M+</p>
            <p className="text-xs text-muted-foreground leading-relaxed">
              Farmers we aim to serve by 2030
            </p>
          </div>
          <div className="border-l border-foreground pl-3">
            <p className="text-2xl font-bold tabular-nums mb-0.5">$100M</p>
            <p className="text-xs text-muted-foreground leading-relaxed">
              In new credit projected within the next three years
            </p>
          </div>
        </div>
      </section>

      {/* Why Microvault */}
      <section className="mb-20">
        <h2 className="text-2xl font-bold leading-snug tracking-tight mb-4">Why Microvault Is Different</h2>
        <p className="text-muted-foreground leading-relaxed max-w-prose">
          Traditional finance asks farmers to prove they are creditworthy using
          documents and collateral they don&apos;t have. Microvault flips the
          model &mdash; using blockchain transparency, AI intelligence, and
          last-mile accessibility to build trust from the ground up. Through
          Stellar&apos;s Soroban smart contracts and the SEP-56 Vault standard,
          every loan is trustless, auditable, and scalable to reach millions of
          farmers at once.
        </p>
      </section>

      {/* Infrastructure Partners */}
      <section className="border-t border-border pt-16 mb-24 grid grid-cols-1 md:grid-cols-5 gap-12 items-start">
        <div className="md:col-span-2 space-y-4">
          <p className="text-xs font-mono uppercase tracking-widest text-muted-foreground">
            Infrastructure
          </p>
          <h2 className="text-2xl font-bold leading-tight tracking-tight text-foreground">
            Engineered with Industry Leaders
          </h2>
          <p className="text-muted-foreground text-sm leading-relaxed max-w-sm">
            Our micro-lending engine doesn&apos;t run in isolation. We bridge international capital pools to the last mile by integrating decentralized ledgers, USSD communication networks, and global payment channels.
          </p>
        </div>

        <div className="md:col-span-3 grid grid-cols-1 sm:grid-cols-2 gap-4">
          <a
            href="https://stellar.org/"
            target="_blank"
            rel="noopener noreferrer"
            className="group block border border-border/80 rounded-xl p-5 hover:bg-muted/10 transition-all duration-300"
          >
            <div className="flex items-center justify-between mb-4">
              <img
                src={stellarLogo}
                alt="Stellar Network"
                loading="lazy"
                className="h-7 w-7 object-contain"
              />
              <ExternalLink className="h-3.5 w-3.5 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
            </div>
            <h3 className="font-semibold text-sm text-foreground mb-1">Stellar Network</h3>
            <p className="text-xs text-muted-foreground leading-relaxed">
              Borderless payments and Soroban smart contracts power transparent,
              auditable DeFi lending via the SEP-56 Vault standard.
            </p>
          </a>

          <a
            href="https://africastalking.com/"
            target="_blank"
            rel="noopener noreferrer"
            className="group block border border-border/80 rounded-xl p-5 hover:bg-muted/10 transition-all duration-300"
          >
            <div className="flex items-center justify-between mb-4">
              <img
                src={atLogo}
                alt="Africa's Talking"
                loading="lazy"
                className="h-7 object-contain"
              />
              <ExternalLink className="h-3.5 w-3.5 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
            </div>
            <h3 className="font-semibold text-sm text-foreground mb-1">
              Africa&apos;s Talking
            </h3>
            <p className="text-xs text-muted-foreground leading-relaxed">
              USSD and SMS mobile services enabling farmers to access loans and
              manage repayments from any phone.
            </p>
          </a>

          <a
            href="https://yellowcard.io/"
            target="_blank"
            rel="noopener noreferrer"
            className="group block border border-border/80 rounded-xl p-5 hover:bg-muted/10 transition-all duration-300"
          >
            <div className="flex items-center justify-between mb-4">
              <img
                src={yellowCardLogo}
                alt="Yellow Card"
                loading="lazy"
                className="h-7 object-contain"
              />
              <ExternalLink className="h-3.5 w-3.5 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
            </div>
            <h3 className="font-semibold text-sm text-foreground mb-1">Yellow Card</h3>
            <p className="text-xs text-muted-foreground leading-relaxed">
              Fiat on/off-ramp infrastructure enabling last-mile loan
              disbursement and repayment across Africa.
            </p>
          </a>

          <a
            href="https://www.moneygram.com/"
            target="_blank"
            rel="noopener noreferrer"
            className="group block border border-border/80 rounded-xl p-5 hover:bg-muted/10 transition-all duration-300"
          >
            <div className="flex items-center justify-between mb-4">
              <img
                src={moneygramLogo}
                alt="MoneyGram"
                loading="lazy"
                className="h-7 w-7 object-contain"
              />
              <ExternalLink className="h-3.5 w-3.5 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
            </div>
            <h3 className="font-semibold text-sm text-foreground mb-1">MoneyGram</h3>
            <p className="text-xs text-muted-foreground leading-relaxed">
              Global cash-pickup network letting borrowers collect loan funds in
              person at thousands of agent locations.
            </p>
          </a>
        </div>
      </section>
    </main>
  );
}

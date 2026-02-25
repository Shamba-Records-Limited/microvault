import { MapPin, Mail, Phone } from "lucide-react";
import { Separator } from "@/components/ui/separator";
import shambaLogo from "@/assets/shamba-logo.svg";

export function Footer() {
  const year = new Date().getFullYear();

  return (
    <footer className="border-t border-border mt-16">
      <div className="container py-10">
        <div className="flex flex-col md:flex-row md:items-start md:justify-between gap-8">
          {/* Branding */}
          <div className="flex flex-col gap-3">
            <a
              href="https://www.shambarecords.com/"
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-3 hover:opacity-80 transition-opacity"
            >
              <div className="h-8 w-24 overflow-hidden rounded bg-white flex items-center justify-center px-1">
                <img
                  src={shambaLogo}
                  alt="Shamba Records"
                  className="h-full w-full object-contain"
                />
              </div>
            </a>
            <p className="text-xs text-muted-foreground max-w-xs">
              Microvault is built by Shamba Records — turning farm data into
              financial identity for emerging markets.
            </p>
          </div>

          {/* Contact */}
          <div className="flex flex-col gap-2.5 text-sm text-muted-foreground">
            <p className="text-xs font-medium uppercase tracking-widest text-foreground mb-1">
              Contact
            </p>
            <a
              href="https://maps.google.com/?q=Mitsumi+Business+Park+Muthithi+Road+Nairobi"
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-2 hover:text-foreground transition-colors"
            >
              <MapPin className="h-3.5 w-3.5 shrink-0" />
              <span>Mitsumi Business Park, Muthithi Road</span>
            </a>
            <a
              href="mailto:info@shambarecords.com"
              className="flex items-center gap-2 hover:text-foreground transition-colors"
            >
              <Mail className="h-3.5 w-3.5 shrink-0" />
              <span>info@shambarecords.com</span>
            </a>
            <a
              href="tel:+254732693963"
              className="flex items-center gap-2 hover:text-foreground transition-colors"
            >
              <Phone className="h-3.5 w-3.5 shrink-0" />
              <span>+254 732 693 963</span>
            </a>
          </div>
        </div>

        <Separator className="my-6" />

        <p className="text-xs text-muted-foreground text-center">
          &copy; {year} Shamba Records. All rights reserved.
        </p>
      </div>
    </footer>
  );
}

import { MapPin, Mail, Phone } from "lucide-react";
import { siFacebook, siX } from "simple-icons";

// LinkedIn was removed from simple-icons; path sourced from LinkedIn brand guidelines
const siLinkedinPath =
  "M20.447 20.452h-3.554v-5.569c0-1.328-.027-3.037-1.852-3.037-1.853 0-2.136 1.445-2.136 2.939v5.667H9.351V9h3.414v1.561h.046c.477-.9 1.637-1.85 3.37-1.85 3.601 0 4.267 2.37 4.267 5.455v6.286zM5.337 7.433a2.062 2.062 0 0 1-2.063-2.065 2.064 2.064 0 1 1 2.063 2.065zm1.782 13.019H3.555V9h3.564v11.452zM22.225 0H1.771C.792 0 0 .774 0 1.729v20.542C0 23.227.792 24 1.771 24h20.451C23.2 24 24 23.227 24 22.271V1.729C24 .774 23.2 0 22.222 0h.003z";
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
            <p className="text-sm text-muted-foreground max-w-xs">
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

        <div className="flex flex-col md:flex-row items-center justify-between gap-4">
          <p className="text-sm text-muted-foreground">
            &copy; {year} Shamba Records. All rights reserved.
          </p>
          <div className="flex items-center gap-4">
            <a
              href="https://www.facebook.com/ShambaRecords"
              target="_blank"
              rel="noopener noreferrer"
              aria-label="Shamba Records on Facebook"
              className="text-muted-foreground hover:text-foreground transition-colors"
            >
              <svg role="img" viewBox="0 0 24 24" className="h-4 w-4 fill-current"><path d={siFacebook.path} /></svg>
            </a>
            <a
              href="https://x.com/RecordsShamba"
              target="_blank"
              rel="noopener noreferrer"
              aria-label="Shamba Records on X"
              className="text-muted-foreground hover:text-foreground transition-colors"
            >
              <svg role="img" viewBox="0 0 24 24" className="h-4 w-4 fill-current"><path d={siX.path} /></svg>
            </a>
            <a
              href="https://www.linkedin.com/company/shamba-records/"
              target="_blank"
              rel="noopener noreferrer"
              aria-label="Shamba Records on LinkedIn"
              className="text-muted-foreground hover:text-foreground transition-colors"
            >
              <svg role="img" viewBox="0 0 24 24" className="h-4 w-4 fill-current"><path d={siLinkedinPath} /></svg>
            </a>
          </div>
        </div>
      </div>
    </footer>
  );
}

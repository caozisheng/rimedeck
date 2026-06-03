import type { Metadata } from "next";
import { ContactSalesPageClient } from "@/features/landing/components/contact-sales-page-client";

export const metadata: Metadata = {
  title: "Contact Sales",
  description:
    "Talk to the RimeDeck team about rolling out human + agent workflows at your company.",
  openGraph: {
    title: "Contact Sales — RimeDeck",
    description:
      "Tell us about your team. We’ll respond within three business days.",
    url: "/contact-sales",
  },
  alternates: {
    canonical: "/contact-sales",
  },
};

export default function ContactSalesPage() {
  return <ContactSalesPageClient />;
}

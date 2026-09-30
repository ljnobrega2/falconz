function whatsappTarget(destination) {
  let url;
  try {
    url = new URL(destination);
  } catch {
    return null;
  }

  const host = url.hostname.toLowerCase();
  let phone = "";
  if (host === "wa.me" || host === "www.wa.me") {
    phone = url.pathname.replace(/^\/+|\/+$/g, "");
  } else if (host === "api.whatsapp.com" || host === "www.api.whatsapp.com") {
    if (url.pathname.replace(/\/+$/g, "") !== "/send") return null;
    phone = url.searchParams.get("phone") || "";
  } else {
    return null;
  }

  if (!/^\d{8,15}$/.test(phone)) return null;
  const text = url.searchParams.get("text") || "";
  const query = `phone=${encodeURIComponent(phone)}${text ? `&text=${encodeURIComponent(text)}` : ""}`;
  return { query };
}

export function redirectDestinationForUserAgent(destination, userAgent = "") {
  const target = whatsappTarget(destination);
  if (!target) return destination;

  if (/Android/i.test(userAgent)) {
    return `intent://send?${target.query}#Intent;scheme=whatsapp;package=com.whatsapp;S.browser_fallback_url=${encodeURIComponent(destination)};end`;
  }
  if (/iPhone|iPad|iPod/i.test(userAgent)) {
    return `whatsapp://send?${target.query}`;
  }
  return destination;
}

export function destinationRedirectResponse(destination, userAgent = "") {
  return new Response(null, {
    status: 302,
    headers: {
      "Cache-Control": "no-store",
      "Location": redirectDestinationForUserAgent(destination, userAgent),
    },
  });
}

const https = require("https");

const fallbackRelease = {
  tagName: "latest",
  url: "https://github.com/blackdragoon26/Do-It/releases/latest",
};

function fetchLatestRelease() {
  return new Promise((resolve, reject) => {
    const request = https.get(
      {
        hostname: "api.github.com",
        path: "/repos/blackdragoon26/Do-It/releases/latest",
        headers: {
          Accept: "application/vnd.github+json",
          "User-Agent": "Do-It-website",
        },
      },
      (response) => {
        let body = "";

        response.setEncoding("utf8");
        response.on("data", (chunk) => {
          body += chunk;
        });
        response.on("end", () => {
          if (response.statusCode < 200 || response.statusCode >= 300) {
            reject(new Error(`GitHub returned ${response.statusCode}`));
            return;
          }

          try {
            resolve(JSON.parse(body));
          } catch (error) {
            reject(error);
          }
        });
      },
    );

    request.setTimeout(3000, () => {
      request.destroy(new Error("GitHub release lookup timed out"));
    });
    request.on("error", reject);
  });
}

module.exports = async function handler(request, response) {
  if (request.method !== "GET" && request.method !== "HEAD") {
    response.setHeader("Allow", "GET, HEAD");
    response.statusCode = 405;
    response.end();
    return;
  }

  try {
    const release = await fetchLatestRelease();
    const payload = {
      tagName: release.tag_name || fallbackRelease.tagName,
      url: release.html_url || fallbackRelease.url,
    };

    response.setHeader("Cache-Control", "s-maxage=60, stale-while-revalidate=300");
    response.setHeader("Content-Type", "application/json; charset=utf-8");
    response.end(JSON.stringify(payload));
  } catch {
    response.setHeader("Cache-Control", "s-maxage=30, stale-while-revalidate=60");
    response.setHeader("Content-Type", "application/json; charset=utf-8");
    response.end(JSON.stringify(fallbackRelease));
  }
};

"""Reuters news article scraper implementation using BeautifulSoup for web content extraction."""

from datetime import datetime
import json
import logging
import os
import time
from urllib.parse import quote, urljoin

from bs4 import BeautifulSoup
import requests
from wilder_dao.wilder_database import WilderDatabase
from wilder_my.my_time import MyTime, ensure_mytime

from wilder_scraper.scraper_dataclasses import ScrapedContent


# Helper function to clean text
def clean_text(text_data):
    if isinstance(text_data, list):
        return [item.strip() for item in text_data if item and item.strip()]
    if isinstance(text_data, str):
        return text_data.strip()
    return text_data


class ReutersNewsScraper:
    def __init__(
        self, config_dir: str | None = None, database: WilderDatabase | None = None
    ):
        """
        Initialize the Reuters scraper with BeautifulSoup.

        Args:
            config_dir: Optional path to config directory. If not provided, uses default location.
        """
        self.logger = logging.getLogger(__name__)
        self.session = requests.Session()
        self.database = database
        # Set comprehensive headers to mimic a real browser
        self.session.headers.update(
            {
                "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
                "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
                "Accept-Language": "en-US,en;q=0.9",
                "Accept-Encoding": "gzip, deflate, br",
                "DNT": "1",
                "Connection": "keep-alive",
                "Upgrade-Insecure-Requests": "1",
                "Sec-Fetch-Dest": "document",
                "Sec-Fetch-Mode": "navigate",
                "Sec-Fetch-Site": "none",
                "Sec-Fetch-User": "?1",
                "Cache-Control": "max-age=0",
                "sec-ch-ua": '"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"',
                "sec-ch-ua-mobile": "?0",
                "sec-ch-ua-platform": '"Windows"',
            }
        )

        # Load selectors
        if config_dir is None:
            current_dir = os.path.dirname(__file__)
            config_dir = os.path.join(current_dir, "..", "config")

        article_config_path = os.path.join(config_dir, "selectors_reuters_article.json")
        index_config_path = os.path.join(config_dir, "selectors_reuters.json")

        with open(article_config_path) as f:
            self.article_selectors = json.load(f)
        with open(index_config_path) as f:
            self.index_selectors = json.load(f)

    def _convert_scrapy_selector(self, selector: str) -> tuple[str, str | None]:
        """
        Convert Scrapy CSS selector to BeautifulSoup format.
        Returns (css_selector, attribute_name) where attribute_name is None for text content.
        """
        if "::text" in selector:
            return selector.replace("::text", "").strip(), "text"
        if "::attr(" in selector:
            # Extract attribute name from ::attr(name)
            css_part = selector.split("::attr(")[0].strip()
            attr_part = selector.split("::attr(")[1].rstrip(")")
            return css_part, attr_part
        return selector, None

    def _extract_with_selector(
        self, soup: BeautifulSoup, selector: str, multiple: bool = False
    ):
        """Extract content using a selector, handling Scrapy-style selectors."""
        css_selector, extract_type = self._convert_scrapy_selector(selector)

        if multiple:
            elements = soup.select(css_selector)
            if extract_type == "text":
                return [
                    elem.get_text(strip=True)
                    for elem in elements
                    if elem.get_text(strip=True)
                ]
            if extract_type:
                return [
                    elem.get(extract_type)
                    for elem in elements
                    if elem.get(extract_type)
                ]
            return elements
        element = soup.select_one(css_selector)
        if not element:
            return None

        if extract_type == "text":
            return element.get_text(strip=True)
        if extract_type:
            return element.get(extract_type)
        return element

    def search_articles(
        self, query: str, max_articles: int | None = None
    ) -> list[ScrapedContent]:
        """
        Search for articles on Reuters.
        """
        articles = []
        base_url = f"https://www.reuters.com/pf/api/v3/content/fetch/articles-by-search-v2?query=%7B%22keyword%22%3A%22{quote(query)}%22%2C%22offset%22%3A0%2C%22orderby%22%3A%22display_date%3Adesc%22%2C%22size%22%3A20%2C%22website%22%3A%22reuters%22%7D&d=288&mxId=00000000&_website=reuters"
        response = self.session.get(base_url, timeout=30)
        if response.status_code in {403, 401}:
            self.logger.error(
                f"Access denied for search {query} (status {response.status_code})"
            )
            return articles
        response.raise_for_status()
        result_json = response.json()
        article_json = result_json["result"]["articles"]
        for article in article_json:
            article_url = article["canonical_url"]
            article_url = urljoin("https://www.reuters.com", article_url)
            if self.database:
                existing_item = self.database.scraped_contents.try_get(
                    ScrapedContent.build_id(article_url, "reuters_article")
                )
                if existing_item:
                    continue
            article_content = self._fetch_article(article_url, depth=1)
            if article_content:
                articles.append(article_content)
            if max_articles and len(articles) >= max_articles:
                break
        return articles

    def fetch_frontpage_articles(
        self,
        base_url: str = "https://www.reuters.com/",
        max_articles: int | None = None,
        only_new_articles: bool = False,
    ) -> list[ScrapedContent]:
        """
        Fetch articles from Reuters front page.

        Args:
            base_url: The Reuters URL to start from (default: Reuters homepage)
            max_articles: Optional limit on number of articles to fetch

        Returns:
            List of ScrapedContent objects
        """
        articles = []
        if only_new_articles and not self.database:
            raise ValueError(
                "only_new_articles is only supported if a database is provided"
            )

        # Fetch the main page
        self.logger.info(f"Fetching main page: {base_url}")
        response = self.session.get(base_url, timeout=30)

        # Log the response status for debugging
        self.logger.info(f"Response status: {response.status_code}")

        if response.status_code in {403, 401}:
            self.logger.error(
                f"Access denied (status {response.status_code}). The website may be blocking automated requests."
            )
            self.logger.info("Consider using a web scraping service or API instead.")
            return articles

        response.raise_for_status()

        soup = BeautifulSoup(response.content, "html.parser")
        return self.extract_articles(soup, max_articles, only_new_articles)

    def extract_articles(
        self,
        soup: BeautifulSoup,
        max_articles: int | None = None,
        only_new_articles: bool = False,
    ) -> list[ScrapedContent]:
        """
        Extract article URLs from a BeautifulSoup object.
        """
        articles = []
        # Extract article URLs from the main page
        article_urls = self._extract_with_selector(
            soup, self.index_selectors["urls"], multiple=True
        )

        if not article_urls:
            self.logger.warning("No article URLs found on the main page")
            return articles

        # Make URLs absolute - ensure we have strings
        article_urls = [
            urljoin("https://www.reuters.com", str(url)) for url in article_urls if url
        ]

        # Limit articles if requested
        if max_articles:
            article_urls = article_urls[:max_articles]

        self.logger.info(f"Found {len(article_urls)} article URLs to fetch")

        # Fetch each article
        for idx, article_url in enumerate(article_urls):
            try:
                # Add delay between requests to be respectful

                self.logger.info(
                    f"Fetching article {idx + 1}/{len(article_urls)}: {article_url}"
                )
                article_content = self._fetch_article(
                    article_url, depth=1, only_if_not_exists=only_new_articles
                )
                if article_content:
                    articles.append(article_content)
            except Exception as e:
                self.logger.exception(f"Error fetching article {article_url}: {e!s}")
                continue

        return articles

    def _fetch_article(
        self, url: str, depth: int = 0, only_if_not_exists: bool = False
    ) -> ScrapedContent | None:
        """
        Fetch and parse a single Reuters article.

        Args:
            url: Article URL
            depth: Crawl depth (0 for direct fetch, 1 for linked from index, etc.)

        Returns:
            ScrapedContent object or None if failed
        """
        try:
            if self.database:
                existing_item = self.database.scraped_contents.try_get(
                    ScrapedContent.build_id(url, "reuters_article")
                )
                if existing_item:
                    if only_if_not_exists:
                        return None
                    return existing_item

            time.sleep(2)  # 2 second delay between articles

            response = self.session.get(url, timeout=30)

            if response.status_code in {403, 401}:
                self.logger.error(
                    f"Access denied for article {url} (status {response.status_code})"
                )
                return None

            response.raise_for_status()

            soup = BeautifulSoup(response.content, "html.parser")

            # Extract article data
            title = self._extract_with_selector(soup, self.article_selectors["title"])
            authors = self._extract_with_selector(
                soup, self.article_selectors["author"], multiple=True
            )
            date_str = self._extract_with_selector(soup, self.article_selectors["date"])
            paragraphs = self._extract_with_selector(
                soup, self.article_selectors["paragraphs"], multiple=True
            )
            authors = (
                list(dict.fromkeys([str(author) for author in authors]))
                if authors
                else []
            )
            # Join paragraphs into full text
            if isinstance(paragraphs, list):
                # Ensure all items are strings
                text_content = "\n\n".join(str(p) for p in paragraphs if p)
            else:
                text_content = str(paragraphs) if paragraphs else ""

            # Parse date if available
            published_date = None
            if date_str:
                try:
                    # Try to parse the date string - adjust format as needed for Reuters
                    published_date = ensure_mytime(date_str)
                except Exception as e:
                    self.logger.warning(f"Could not parse date '{date_str}': {e}")

            # Create ScrapedContent object - ensure proper string types
            scraped_content = ScrapedContent(
                url=url,
                scrape_type="reuters_article",
                scraped_at=MyTime.from_datetime(datetime.utcnow()),
                depth=depth,
                content=text_content,  # Full HTML
                title=str(title) if title else None,
                markdown_content=None,  # Could be added later
                summary=None,  # Could be added later
                authors=authors,
                published_date=published_date,
            )
            if self.database:
                self.database.scraped_contents.insert(scraped_content)
            return scraped_content

        except Exception as e:
            self.logger.exception(f"Error parsing article {url}: {e!s}")
            return None


# Example usage
if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    scraper = ReutersNewsScraper()
    articles = scraper.fetch_frontpage_articles(max_articles=5)

    for article in articles:
        print(f"Title: {article.title}")
        print(f"Authors: {', '.join(article.authors) if article.authors else 'None'}")
        print(f"URL: {article.url}")
        print(f"Published: {article.published_date}")
        print("-" * 80)

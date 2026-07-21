from typing import TYPE_CHECKING

from wilder_dao.wilder_database import WilderDatabase
from wilder_scraper.spiders.reuters_news_spider import ReutersNewsScraper

from wilder_agent.agent_dataclasses import ToolResult
from wilder_agent.tools.agent_tool_annotation import AgentTool, param


if TYPE_CHECKING:
    from wilder_scraper.scraper_dataclasses import ScrapedContent


class ReutersNewsTools:
    """Reuters news tools for fetching and searching news articles."""

    def __init__(self, database: WilderDatabase):
        self.database = database

    @AgentTool(
        name="reuters_news",
        description="Use this to get the latest news from Reuters. This tool fetches current information from the Reuters frontpage. Use this when you need up-to-date news and current events.",
    )
    @param(
        "only_new_articles",
        description="Only return new articles not fetched before",
        default=False,
    )
    async def get_reuters_news(self, only_new_articles: bool = False, *, prompt_id: str) -> ToolResult:
        """Get the latest news from Reuters."""

        reuters_spider = ReutersNewsScraper(database=self.database)
        reuters_articles: list[ScrapedContent] = reuters_spider.fetch_frontpage_articles(
            only_new_articles=only_new_articles
        )
        return ToolResult.for_dict(
            prompt_id=prompt_id,
            tool_name="reuters_news",
            summary=f"Found {len(reuters_articles)} Reuters articles",
            result={
                "reuters_articles": reuters_articles,
            },
        )

    @AgentTool(
        name="reuters_news_search",
        description="Use this to search for news from Reuters. This tool finds news articles that match a specific query. Use this when you need to find news about a particular topic or search term.",
    )
    @param("query", description="The query to search for")
    @param(
        "max_articles",
        description="The maximum number of articles to return",
        default=10,
        minimum=1,
        maximum=50,
    )
    async def search_reuters_news(self, query: str, max_articles: int = 10, *, prompt_id: str) -> ToolResult:
        """Search for news from Reuters."""

        reuters_spider = ReutersNewsScraper(database=self.database)
        reuters_articles: list[ScrapedContent] = reuters_spider.search_articles(query, max_articles)
        return ToolResult.for_dict(
            prompt_id=prompt_id,
            tool_name="reuters_news_search",
            summary=f"Found {len(reuters_articles)} Reuters articles for query '{query}'",
            result={
                "reuters_articles": reuters_articles,
            },
        )

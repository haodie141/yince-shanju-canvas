import { Modal } from "antd";
import { ArrowUpRight } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { getFeaturedCanvases, type FeaturedCanvas } from "@/services/api/featured";
import "./featured-canvas-gallery.css";

function FeaturedPreview({ url, cover }: { url?: string; cover?: string }) {
    const ref = useRef<HTMLVideoElement>(null);
    const [nearby, setNearby] = useState(false);
    const [playing, setPlaying] = useState(false);
    useEffect(() => {
        const video = ref.current;
        if (!video) return;
        const observer = new IntersectionObserver(
            ([entry]) => {
                if (entry.isIntersecting) {
                    setNearby(true);
                    observer.disconnect();
                }
            },
            { rootMargin: "200px" },
        );
        observer.observe(video);
        return () => observer.disconnect();
    }, []);
    return (
        <>
            {/* 自定义封面常驻在底层，视频真正开始播放后才淡入，避免悬停时露出黑屏。 */}
            {cover ? <img className="featured-card-cover" src={cover} alt="" loading="lazy" decoding="async" aria-hidden="true" /> : null}
            <video
                ref={ref}
                src={nearby ? url : undefined}
                preload="metadata"
                muted
                loop
                playsInline
                aria-hidden="true"
                onPlaying={() => setPlaying(true)}
                onPause={() => setPlaying(false)}
                className={`featured-card-video${cover ? " is-behind-cover" : ""}${playing ? " is-playing" : ""}`}
            />
        </>
    );
}

const canHoverPreview = () => typeof window === "undefined" || !window.matchMedia || window.matchMedia("(hover: hover)").matches;
const playPreview = (element: HTMLElement) => {
    void element
        .querySelector("video")
        ?.play()
        .catch(() => {});
};
const pausePreview = (element: HTMLElement) => {
    const video = element.querySelector("video");
    if (!video) return;
    video.pause();
    video.currentTime = 0;
};

export default function FeaturedCanvasGallery() {
    const { data } = useQuery({ queryKey: ["featured-canvases"], queryFn: getFeaturedCanvases });
    const [selected, setSelected] = useState<FeaturedCanvas | null>(null);
    if (!data?.items?.length) return null;
    return (
        <section className="featured-canvas" aria-labelledby="featured-canvas-title">
            <header className="featured-canvas-heading">
                <h2 id="featured-canvas-title">创作精选</h2>
            </header>
            <div className="featured-canvas-grid">
                {data.items.map((item) => (
                    <article key={item.id} className="featured-card">
                        <button
                            type="button"
                            className="featured-card-media"
                            onClick={() => setSelected(item)}
                            aria-label={`播放精选作品：${item.title}`}
                            aria-haspopup="dialog"
                            onMouseEnter={(event) => {
                                if (canHoverPreview()) playPreview(event.currentTarget);
                            }}
                            onMouseLeave={(event) => pausePreview(event.currentTarget)}
                            onFocus={(event) => playPreview(event.currentTarget)}
                            onBlur={(event) => pausePreview(event.currentTarget)}
                        >
                            <FeaturedPreview url={item.videoUrl} cover={item.coverUrl} />
                        </button>
                        <div className="featured-card-body">
                            <div className="featured-card-copy">
                                <h3 className="featured-card-title">{item.title}</h3>
                                {item.category ? <p className="featured-card-category">{item.category}</p> : null}
                            </div>
                            <Link to={item.canvasUrl || "#"} className="featured-card-action" aria-label={`查看 ${item.title} 的创作过程`}>
                                <span>查看创作过程</span>
                                <ArrowUpRight className="featured-card-action-icon" aria-hidden="true" />
                            </Link>
                        </div>
                    </article>
                ))}
            </div>
            <Modal open={Boolean(selected)} title={selected?.title} footer={null} onCancel={() => setSelected(null)} destroyOnClose width={920} centered>
                {selected ? (
                    <div className="featured-detail">
                        <video key={selected.id} src={selected.videoUrl} controls autoPlay playsInline className="featured-detail-video" />
                        {selected.description ? <p className="featured-detail-description">{selected.description}</p> : null}
                        <Link to={selected.canvasUrl || "#"} className="featured-detail-action">
                            查看创作过程 <ArrowUpRight className="size-4" aria-hidden="true" />
                        </Link>
                    </div>
                ) : null}
            </Modal>
        </section>
    );
}

package com.android.webrtcbench.motion;

import android.app.Activity;
import android.content.Context;
import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.Paint;
import android.graphics.RectF;
import android.os.Bundle;
import android.util.Log;
import android.view.Choreographer;
import android.view.View;
import android.view.WindowManager;

public final class MainActivity extends Activity {
    private static final String LOG_TAG = "WebRtcMotion";
    private MotionView motionView;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        getWindow().getDecorView().setSystemUiVisibility(
                View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                        | View.SYSTEM_UI_FLAG_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                        | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
                        | View.SYSTEM_UI_FLAG_LAYOUT_STABLE);
        motionView = new MotionView(this);
        setContentView(motionView);
    }

    private static final class MotionView extends View implements Choreographer.FrameCallback {
        private final Paint paint = new Paint(Paint.ANTI_ALIAS_FLAG);
        private final RectF rect = new RectF();
        private long frameCount;
        private long windowStart = System.nanoTime();
        private float measuredFps;
        private float phase;

        MotionView(Context context) {
            super(context);
            setBackgroundColor(Color.BLACK);
            paint.setStyle(Paint.Style.FILL);
        }

        @Override
        protected void onAttachedToWindow() {
            super.onAttachedToWindow();
            Choreographer.getInstance().postFrameCallback(this);
        }

        @Override
        protected void onDetachedFromWindow() {
            Choreographer.getInstance().removeFrameCallback(this);
            super.onDetachedFromWindow();
        }

        @Override
        public void doFrame(long frameTimeNanos) {
            frameCount++;
            long now = System.nanoTime();
            if (now - windowStart >= 1_000_000_000L) {
                measuredFps = frameCount * 1_000_000_000.0f / (now - windowStart);
                Log.i(LOG_TAG, String.format(java.util.Locale.US,
                        "measured_fps=%.3f width=%d height=%d", measuredFps, getWidth(), getHeight()));
                frameCount = 0;
                windowStart = now;
            }
            phase += 0.045f;
            invalidate();
            Choreographer.getInstance().postFrameCallback(this);
        }

        @Override
        protected void onDraw(Canvas canvas) {
            super.onDraw(canvas);
            float width = getWidth();
            float height = getHeight();

            int[] colors = {
                    Color.rgb(5, 8, 20), Color.rgb(16, 34, 80), Color.rgb(6, 74, 83),
                    Color.rgb(15, 112, 90), Color.rgb(87, 38, 120), Color.rgb(143, 35, 70)
            };
            for (int i = 0; i < 12; i++) {
                float offset = (float) Math.sin(phase * (0.7f + i * 0.04f) + i) * width * 0.12f;
                float y = (i / 12f) * height;
                paint.setColor(colors[i % colors.length]);
                paint.setAlpha(150 + (i % 3) * 35);
                rect.set(-width * 0.2f + offset, y, width * 1.1f + offset, y + height / 10f);
                canvas.drawRect(rect, paint);
            }

            for (int i = 0; i < 24; i++) {
                float x = width * (0.5f + 0.42f * (float) Math.sin(phase * 1.8f + i * 0.41f));
                float y = height * (0.5f + 0.42f * (float) Math.cos(phase * 1.35f + i * 0.29f));
                float radius = Math.min(width, height) * (0.012f + (i % 5) * 0.006f);
                paint.setColor(Color.HSVToColor(new float[]{(i * 37f + phase * 80f) % 360f, 0.85f, 1f}));
                paint.setAlpha(225);
                canvas.drawCircle(x, y, radius, paint);
            }

            paint.setColor(Color.WHITE);
            paint.setAlpha(255);
            paint.setTextSize(Math.max(28f, width * 0.045f));
            paint.setTextAlign(Paint.Align.CENTER);
            canvas.drawText(String.format(java.util.Locale.US, "WebRTC motion  %.1f FPS", measuredFps), width / 2f, height * 0.52f, paint);
            paint.setTextSize(Math.max(18f, width * 0.025f));
            canvas.drawText(width + " x " + height, width / 2f, height * 0.57f, paint);
        }
    }
}

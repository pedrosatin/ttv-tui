#include "bridge.h"

static Uint32 EV_MPV, EV_TUI, EV_CTL;
void reg_events(void) { EV_MPV = SDL_RegisterEvents(3); EV_TUI = EV_MPV + 1; EV_CTL = EV_MPV + 2; }
Uint32 ev_mpv(void) { return EV_MPV; }
Uint32 ev_tui(void) { return EV_TUI; }
Uint32 ev_ctl(void) { return EV_CTL; }
void push_ev(Uint32 t) { SDL_Event e; SDL_zero(e); e.type = t; SDL_PushEvent(&e); }
void push_quit(void) { SDL_Event e; SDL_zero(e); e.type = SDL_EVENT_QUIT; SDL_PushEvent(&e); }
void inject_key(SDL_Keycode k, SDL_Keymod m) {
    SDL_Event e; SDL_zero(e); e.type = SDL_EVENT_KEY_DOWN; e.key.key = k; e.key.mod = m; e.key.down = true;
    SDL_PushEvent(&e);
}
Uint32 e_type(SDL_Event *e) { return e->type; }
SDL_Keycode e_key(SDL_Event *e) { return e->key.key; }
SDL_Keymod e_mod(SDL_Event *e) { return e->key.mod; }
const char *e_text(SDL_Event *e) { return e->text.text; }
static void on_update(void *c) { push_ev(EV_MPV); }
static void *gpa(void *c, const char *n) { return (void *)SDL_GL_GetProcAddress(n); }

SDL_Window *make_window(const char *title) {
    return SDL_CreateWindow(title, 1280, 720, SDL_WINDOW_OPENGL | SDL_WINDOW_RESIZABLE | SDL_WINDOW_HIGH_PIXEL_DENSITY);
}
mpv_render_context *make_rc(mpv_handle *m, SDL_Window *win) {
    void *wl = SDL_GetPointerProperty(SDL_GetWindowProperties(win), SDL_PROP_WINDOW_WAYLAND_DISPLAY_POINTER, NULL);
    mpv_opengl_init_params gi = {.get_proc_address = gpa};
    int adv = 1;
    mpv_render_param p[6];
    int n = 0;
    p[n++] = (mpv_render_param){MPV_RENDER_PARAM_API_TYPE, (void *)MPV_RENDER_API_TYPE_OPENGL};
    p[n++] = (mpv_render_param){MPV_RENDER_PARAM_OPENGL_INIT_PARAMS, &gi};
    if (wl != NULL) {
        p[n++] = (mpv_render_param){MPV_RENDER_PARAM_WL_DISPLAY, wl};
    }
    p[n++] = (mpv_render_param){MPV_RENDER_PARAM_ADVANCED_CONTROL, &adv};
    p[n++] = (mpv_render_param){0, NULL};
    mpv_render_context *rc = NULL;
    if (mpv_render_context_create(&rc, m, p) < 0) return NULL;
    mpv_render_context_set_update_callback(rc, on_update, NULL);
    return rc;
}
int rc_has_frame(mpv_render_context *rc) { return (mpv_render_context_update(rc) & MPV_RENDER_UPDATE_FRAME) != 0; }
void rc_render(mpv_render_context *rc, int w, int h) {
    mpv_opengl_fbo fbo = {.fbo = 0, .w = w, .h = h};
    int flip = 1;
    mpv_render_param p[] = {{MPV_RENDER_PARAM_OPENGL_FBO, &fbo}, {MPV_RENDER_PARAM_FLIP_Y, &flip}, {0, NULL}};
    mpv_render_context_render(rc, p);
}
void rc_free(mpv_render_context *rc) { mpv_render_context_set_update_callback(rc, NULL, NULL); mpv_render_context_free(rc); }
void load_fd(mpv_handle *m, const char *url) { const char *a[] = {"loadfile", url, NULL}; mpv_command_async(m, 0, a); }
void set_property(mpv_handle *m, const char *name, const char *value) {
    // A API copia a string antes de retornar; o core aplica fora da thread de render.
    mpv_set_property_async(m, 0, name, MPV_FORMAT_STRING, &value);
}

#ifdef _WIN32
static PFNGLBINDFRAMEBUFFERPROC p_glBindFramebuffer = NULL;
static PFNGLBINDVERTEXARRAYPROC p_glBindVertexArray = NULL;
static PFNGLBINDBUFFERPROC p_glBindBuffer = NULL;
static PFNGLACTIVETEXTUREPROC p_glActiveTexture = NULL;
static PFNGLUSEPROGRAMPROC p_glUseProgram = NULL;

void init_gl_extensions(void) {
    p_glBindFramebuffer = (PFNGLBINDFRAMEBUFFERPROC)SDL_GL_GetProcAddress("glBindFramebuffer");
    p_glBindVertexArray = (PFNGLBINDVERTEXARRAYPROC)SDL_GL_GetProcAddress("glBindVertexArray");
    p_glBindBuffer = (PFNGLBINDBUFFERPROC)SDL_GL_GetProcAddress("glBindBuffer");
    p_glActiveTexture = (PFNGLACTIVETEXTUREPROC)SDL_GL_GetProcAddress("glActiveTexture");
    p_glUseProgram = (PFNGLUSEPROGRAMPROC)SDL_GL_GetProcAddress("glUseProgram");
}

static void gl_reset(int W, int H) {
    if (p_glBindFramebuffer) p_glBindFramebuffer(GL_FRAMEBUFFER, 0);
    if (p_glBindVertexArray) p_glBindVertexArray(0);
    if (p_glBindBuffer) {
        p_glBindBuffer(GL_ARRAY_BUFFER, 0);
        p_glBindBuffer(GL_PIXEL_UNPACK_BUFFER, 0);
    }
    if (p_glActiveTexture) p_glActiveTexture(GL_TEXTURE0);
    if (p_glUseProgram) p_glUseProgram(0);
    glPixelStorei(GL_UNPACK_ROW_LENGTH, 0);
    glPixelStorei(GL_UNPACK_ALIGNMENT, 4);
    glViewport(0, 0, W, H);
}
#else
void init_gl_extensions(void) {}

static void gl_reset(int W, int H) {
    glBindFramebuffer(GL_FRAMEBUFFER, 0);
    glBindVertexArray(0);
    glBindBuffer(GL_ARRAY_BUFFER, 0);
    glBindBuffer(GL_PIXEL_UNPACK_BUFFER, 0);
    glActiveTexture(GL_TEXTURE0);
    glUseProgram(0);
    glPixelStorei(GL_UNPACK_ROW_LENGTH, 0);
    glPixelStorei(GL_UNPACK_ALIGNMENT, 4);
    glViewport(0, 0, W, H);
}
#endif
GLuint make_tex(void) {
    GLuint t; glGenTextures(1, &t); glBindTexture(GL_TEXTURE_2D, t);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
    return t;
}
void tex_alloc(GLuint t, int w, int h) {
    glBindTexture(GL_TEXTURE_2D, t);
    glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA8, w, h, 0, GL_BGRA, GL_UNSIGNED_BYTE, NULL);
}
void tex_rows(GLuint t, int y, int w, int h, const void *p) {
    glBindTexture(GL_TEXTURE_2D, t);
    glTexSubImage2D(GL_TEXTURE_2D, 0, 0, y, w, h, GL_BGRA, GL_UNSIGNED_BYTE, p);
}
void tex_rect(GLuint t, int x, int y, int w, int h, int stride_px, const void *p) {
    glBindTexture(GL_TEXTURE_2D, t);
    glPixelStorei(GL_UNPACK_ROW_LENGTH, stride_px);
    glTexSubImage2D(GL_TEXTURE_2D, 0, x, y, w, h, GL_BGRA, GL_UNSIGNED_BYTE, p);
    glPixelStorei(GL_UNPACK_ROW_LENGTH, 0);
}
void clear_to(int W, int H, float r, float g, float b) {
    gl_reset(W, H); glClearColor(r, g, b, 1); glClear(GL_COLOR_BUFFER_BIT);
}
void draw_panel(GLuint t, int W, int H, int x, int y, int w, int h) {
    gl_reset(W, H);
    glMatrixMode(GL_PROJECTION); glLoadIdentity(); glOrtho(0, W, H, 0, -1, 1);
    glMatrixMode(GL_MODELVIEW); glLoadIdentity();
    glEnable(GL_TEXTURE_2D); glEnable(GL_BLEND); glBlendFunc(GL_ONE, GL_ONE_MINUS_SRC_ALPHA);
    glBindTexture(GL_TEXTURE_2D, t);
    glBegin(GL_QUADS);
    glTexCoord2f(0, 0); glVertex2i(x, y); glTexCoord2f(1, 0); glVertex2i(x + w, y);
    glTexCoord2f(1, 1); glVertex2i(x + w, y + h); glTexCoord2f(0, 1); glVertex2i(x, y + h);
    glEnd();
    glDisable(GL_BLEND);
    glDisable(GL_TEXTURE_2D);
}
// fb_pixel lê um pixel do buffer de desenho, com a origem do OpenGL (embaixo, à esquerda).
void fb_pixel(int x, int y, unsigned char *rgba) {
    glReadPixels(x, y, 1, 1, GL_RGBA, GL_UNSIGNED_BYTE, rgba);
}

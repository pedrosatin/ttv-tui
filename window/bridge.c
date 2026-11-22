#include "bridge.h"

static Uint32 EV_MPV, EV_TUI, EV_CTL, EV_INPUT;
void reg_events(void) { EV_MPV = SDL_RegisterEvents(4); EV_TUI = EV_MPV + 1; EV_CTL = EV_MPV + 2; EV_INPUT = EV_MPV + 3; }
Uint32 ev_mpv(void) { return EV_MPV; }
Uint32 ev_tui(void) { return EV_TUI; }
Uint32 ev_ctl(void) { return EV_CTL; }
Uint32 ev_input(void) { return EV_INPUT; }
/* Bytes do socket de teste para o PTY, pela mesma fila das teclas: a ordem entre
   key e text fica a do pedido. A cópia é liberada por take_input. */
void inject_input(const void *p, int n) {
    SDL_Event e; SDL_zero(e); e.type = EV_INPUT;
    void *copy = SDL_malloc(n > 0 ? n : 1);
    if (copy == NULL) return;
    SDL_memcpy(copy, p, n);
    e.user.data1 = copy; e.user.code = n;
    if (!SDL_PushEvent(&e)) SDL_free(copy);
}
void *input_data(SDL_Event *e) { return e->user.data1; }
int input_len(SDL_Event *e) { return e->user.code; }
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
static PFNGLGENFRAMEBUFFERSPROC p_glGenFramebuffers = NULL;
static PFNGLDELETEFRAMEBUFFERSPROC p_glDeleteFramebuffers = NULL;
static PFNGLFRAMEBUFFERTEXTURE2DPROC p_glFramebufferTexture2D = NULL;
static PFNGLCHECKFRAMEBUFFERSTATUSPROC p_glCheckFramebufferStatus = NULL;
static PFNGLBLITFRAMEBUFFERPROC p_glBlitFramebuffer = NULL;
static PFNGLBINDVERTEXARRAYPROC p_glBindVertexArray = NULL;
static PFNGLBINDBUFFERPROC p_glBindBuffer = NULL;
static PFNGLACTIVETEXTUREPROC p_glActiveTexture = NULL;
static PFNGLUSEPROGRAMPROC p_glUseProgram = NULL;

void init_gl_extensions(void) {
    p_glBindFramebuffer = (PFNGLBINDFRAMEBUFFERPROC)SDL_GL_GetProcAddress("glBindFramebuffer");
    p_glGenFramebuffers = (PFNGLGENFRAMEBUFFERSPROC)SDL_GL_GetProcAddress("glGenFramebuffers");
    p_glDeleteFramebuffers = (PFNGLDELETEFRAMEBUFFERSPROC)SDL_GL_GetProcAddress("glDeleteFramebuffers");
    p_glFramebufferTexture2D = (PFNGLFRAMEBUFFERTEXTURE2DPROC)SDL_GL_GetProcAddress("glFramebufferTexture2D");
    p_glCheckFramebufferStatus = (PFNGLCHECKFRAMEBUFFERSTATUSPROC)SDL_GL_GetProcAddress("glCheckFramebufferStatus");
    p_glBlitFramebuffer = (PFNGLBLITFRAMEBUFFERPROC)SDL_GL_GetProcAddress("glBlitFramebuffer");
    p_glBindVertexArray = (PFNGLBINDVERTEXARRAYPROC)SDL_GL_GetProcAddress("glBindVertexArray");
    p_glBindBuffer = (PFNGLBINDBUFFERPROC)SDL_GL_GetProcAddress("glBindBuffer");
    p_glActiveTexture = (PFNGLACTIVETEXTUREPROC)SDL_GL_GetProcAddress("glActiveTexture");
    p_glUseProgram = (PFNGLUSEPROGRAMPROC)SDL_GL_GetProcAddress("glUseProgram");
}

static void gl_bind_fb(GLenum target, GLuint fb) {
    if (p_glBindFramebuffer) p_glBindFramebuffer(target, fb);
}
static void gl_gen_fbs(GLsizei n, GLuint *ids) {
    if (p_glGenFramebuffers) p_glGenFramebuffers(n, ids);
}
static void gl_del_fbs(GLsizei n, const GLuint *ids) {
    if (p_glDeleteFramebuffers) p_glDeleteFramebuffers(n, ids);
}
static void gl_fb_tex2d(GLenum target, GLenum attachment, GLenum textarget, GLuint texture, GLint level) {
    if (p_glFramebufferTexture2D) p_glFramebufferTexture2D(target, attachment, textarget, texture, level);
}
static GLenum gl_fb_status(GLenum target) {
    return p_glCheckFramebufferStatus ? p_glCheckFramebufferStatus(target) : 0;
}
static void gl_blit_fb(GLint srcX0, GLint srcY0, GLint srcX1, GLint srcY1, GLint dstX0, GLint dstY0, GLint dstX1, GLint dstY1, GLbitfield mask, GLenum filter) {
    if (p_glBlitFramebuffer) p_glBlitFramebuffer(srcX0, srcY0, srcX1, srcY1, dstX0, dstY0, dstX1, dstY1, mask, filter);
}

static void gl_reset(int W, int H) {
    gl_bind_fb(GL_FRAMEBUFFER, 0);
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

static void gl_bind_fb(GLenum target, GLuint fb) { glBindFramebuffer(target, fb); }
static void gl_gen_fbs(GLsizei n, GLuint *ids) { glGenFramebuffers(n, ids); }
static void gl_del_fbs(GLsizei n, const GLuint *ids) { glDeleteFramebuffers(n, ids); }
static void gl_fb_tex2d(GLenum target, GLenum attachment, GLenum textarget, GLuint texture, GLint level) {
    glFramebufferTexture2D(target, attachment, textarget, texture, level);
}
static GLenum gl_fb_status(GLenum target) { return glCheckFramebufferStatus(target); }
static void gl_blit_fb(GLint srcX0, GLint srcY0, GLint srcX1, GLint srcY1, GLint dstX0, GLint dstY0, GLint dstX1, GLint dstY1, GLbitfield mask, GLenum filter) {
    glBlitFramebuffer(srcX0, srcY0, srcX1, srcY1, dstX0, dstY0, dstX1, dstY1, mask, filter);
}

static void gl_reset(int W, int H) {
    gl_bind_fb(GL_FRAMEBUFFER, 0);
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

/* Alvo offscreen da prévia: um FBO + textura RGBA do tamanho do quadro PiP. */
static GLuint pip_fbo = 0, pip_tex = 0;
static int pip_w = 0, pip_h = 0;

// Tudo daqui para baixo é o caminho do painel e só usa OpenGL 1.1, que é o que o
// opengl32.dll genérico do Windows oferece. O que passa disso (lados fora de potência de 2,
// GL_BGRA, textura maior que o limite) é decidido em paneltex.go.
const char *gl_str(GLenum name) { const GLubyte *s = glGetString(name); return s ? (const char *)s : ""; }
int gl_max_tex(void) { GLint n = 0; glGetIntegerv(GL_MAX_TEXTURE_SIZE, &n); return n; }
// gl_error devolve o primeiro erro pendente e esvazia a fila.
int gl_error(void) {
    GLenum first = glGetError();
    for (int i = 0; first != GL_NO_ERROR && i < 16 && glGetError() != GL_NO_ERROR; i++) {}
    return (int)first;
}
GLuint make_tex(void) {
    GLuint t; glGenTextures(1, &t); glBindTexture(GL_TEXTURE_2D, t);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
    return t;
}
void free_tex(GLuint t) { glDeleteTextures(1, &t); }

void pip_fbo_free(void) {
    if (pip_fbo != 0) {
        gl_del_fbs(1, &pip_fbo);
        pip_fbo = 0;
    }
    if (pip_tex != 0) {
        free_tex(pip_tex);
        pip_tex = 0;
    }
    pip_w = pip_h = 0;
}

int pip_fbo_ensure(int w, int h) {
    if (w <= 0 || h <= 0) {
        return -1;
    }
    if (pip_fbo != 0 && pip_w == w && pip_h == h) {
        return 0;
    }
    pip_fbo_free();
    pip_tex = make_tex();
    glBindTexture(GL_TEXTURE_2D, pip_tex);
    glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA8, w, h, 0, GL_RGBA, GL_UNSIGNED_BYTE, NULL);
    gl_gen_fbs(1, &pip_fbo);
    if (pip_fbo == 0 || pip_tex == 0) {
        pip_fbo_free();
        return -1;
    }
    gl_bind_fb(GL_FRAMEBUFFER, pip_fbo);
    gl_fb_tex2d(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, pip_tex, 0);
    if (gl_fb_status(GL_FRAMEBUFFER) != GL_FRAMEBUFFER_COMPLETE) {
        gl_bind_fb(GL_FRAMEBUFFER, 0);
        pip_fbo_free();
        return -1;
    }
    gl_bind_fb(GL_FRAMEBUFFER, 0);
    pip_w = w;
    pip_h = h;
    return 1;
}

void rc_render_to_pip(mpv_render_context *rc) {
    if (rc == NULL || pip_fbo == 0 || pip_w <= 0 || pip_h <= 0) {
        return;
    }
    gl_bind_fb(GL_FRAMEBUFFER, pip_fbo);
    glViewport(0, 0, pip_w, pip_h);
    mpv_opengl_fbo fbo = {.fbo = (int)pip_fbo, .w = pip_w, .h = pip_h};
    int flip = 1;
    mpv_render_param p[] = {{MPV_RENDER_PARAM_OPENGL_FBO, &fbo}, {MPV_RENDER_PARAM_FLIP_Y, &flip}, {0, NULL}};
    mpv_render_context_render(rc, p);
    gl_bind_fb(GL_FRAMEBUFFER, 0);
}

void pip_compose(int x, int y, int rw, int rh, int win_w, int win_h) {
    if (pip_fbo == 0 || pip_w <= 0 || pip_h <= 0 || rw <= 0 || rh <= 0 || win_w <= 0 || win_h <= 0) {
        return;
    }
    /* y da janela é em cima à esquerda; blit usa origem embaixo à esquerda.
       O FBO tem o mesmo layout W×H da janela (prévia já nas margens do PiP). */
    int y0 = win_h - y - rh;
    int y1 = win_h - y;
    gl_bind_fb(GL_READ_FRAMEBUFFER, pip_fbo);
    gl_bind_fb(GL_DRAW_FRAMEBUFFER, 0);
    gl_blit_fb(x, y0, x + rw, y1, x, y0, x + rw, y1, GL_COLOR_BUFFER_BIT, GL_NEAREST);
    gl_bind_fb(GL_FRAMEBUFFER, 0);
    glViewport(0, 0, win_w, win_h);
}

// O formato dos pixels é validado mesmo sem dados, então a alocação também segue bgra.
void tex_alloc(GLuint t, int w, int h, int bgra) {
    glBindTexture(GL_TEXTURE_2D, t);
    glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA8, w, h, 0, bgra ? GL_BGRA : GL_RGBA, GL_UNSIGNED_BYTE, NULL);
}
void tex_rect(GLuint t, int x, int y, int w, int h, int stride_px, const void *p, int bgra) {
    glBindTexture(GL_TEXTURE_2D, t);
    glPixelStorei(GL_UNPACK_ROW_LENGTH, stride_px);
    glTexSubImage2D(GL_TEXTURE_2D, 0, x, y, w, h, bgra ? GL_BGRA : GL_RGBA, GL_UNSIGNED_BYTE, p);
    glPixelStorei(GL_UNPACK_ROW_LENGTH, 0);
}
void clear_to(int W, int H, float r, float g, float b) {
    gl_reset(W, H); glClearColor(r, g, b, 1); glClear(GL_COLOR_BUFFER_BIT);
}
void panel_begin(int W, int H) {
    gl_reset(W, H);
    glMatrixMode(GL_PROJECTION); glLoadIdentity(); glOrtho(0, W, H, 0, -1, 1);
    glMatrixMode(GL_MODELVIEW); glLoadIdentity();
    glEnable(GL_TEXTURE_2D); glEnable(GL_BLEND); glBlendFunc(GL_ONE, GL_ONE_MINUS_SRC_ALPHA);
}
// panel_tile desenha um ladrilho: u e v são onde o painel acaba dentro da textura.
void panel_tile(GLuint t, int x, int y, int w, int h, float u, float v) {
    glBindTexture(GL_TEXTURE_2D, t);
    glBegin(GL_QUADS);
    glTexCoord2f(0, 0); glVertex2i(x, y); glTexCoord2f(u, 0); glVertex2i(x + w, y);
    glTexCoord2f(u, v); glVertex2i(x + w, y + h); glTexCoord2f(0, v); glVertex2i(x, y + h);
    glEnd();
}
void panel_end(void) {
    glDisable(GL_BLEND);
    glDisable(GL_TEXTURE_2D);
}
// fb_pixel lê um pixel do buffer de desenho, com a origem do OpenGL (embaixo, à esquerda).
void fb_pixel(int x, int y, unsigned char *rgba) {
    glReadPixels(x, y, 1, 1, GL_RGBA, GL_UNSIGNED_BYTE, rgba);
}

bool e_repeat(SDL_Event *e) { return e->key.repeat; }

void inject_repeat_burst(SDL_Keycode k, int count) {
    SDL_Event e; SDL_zero(e); e.type = SDL_EVENT_KEY_DOWN; e.key.key = k; e.key.down = true;
    SDL_PushEvent(&e);
    e.key.repeat = true;
    for (int i = 0; i < count; i++) SDL_PushEvent(&e);
    e.type = SDL_EVENT_KEY_UP; e.key.down = false; e.key.repeat = false;
    SDL_PushEvent(&e);
}

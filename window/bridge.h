#ifndef TTV_TUI_WINDOW_BRIDGE_H
#define TTV_TUI_WINDOW_BRIDGE_H

#ifndef GL_GLEXT_PROTOTYPES
#ifndef _WIN32
#define GL_GLEXT_PROTOTYPES 1
#endif
#endif
#include <SDL3/SDL.h>
#include <SDL3/SDL_opengl.h>
#include <mpv/client.h>
#include <mpv/render_gl.h>
#include <stdlib.h>

void init_gl_extensions(void);

void reg_events(void);
Uint32 ev_mpv(void);
Uint32 ev_tui(void);
Uint32 ev_ctl(void);
Uint32 ev_input(void);
void inject_input(const void *p, int n);
void *input_data(SDL_Event *e);
int input_len(SDL_Event *e);
void push_ev(Uint32 t);
void push_quit(void);
void inject_key(SDL_Keycode k, SDL_Keymod m);
Uint32 e_type(SDL_Event *e);
SDL_Keycode e_key(SDL_Event *e);
SDL_Keymod e_mod(SDL_Event *e);
const char *e_text(SDL_Event *e);
SDL_Window *make_window(const char *title);
mpv_render_context *make_rc(mpv_handle *m, SDL_Window *win);
int rc_has_frame(mpv_render_context *rc);
void rc_render(mpv_render_context *rc, int w, int h);
// FBO offscreen da prévia (PiP): mpv_render apaga o scissor e o FBO 0 inteiro,
// então a prévia desenha num alvo W×H próprio (com as mesmas margens) e só o
// retângulo do quadro é composto depois. pip_fbo_ensure devolve 0 se reusou,
// 1 se recriou, -1 se falhou.
int pip_fbo_ensure(int w, int h);
void pip_fbo_free(void);
void rc_render_to_pip(mpv_render_context *rc);
// pip_compose copia o retângulo (x,y,rw,rh) do FBO da prévia para o FBO 0
// (origem em cima à esquerda); restaura FBO 0 e o viewport.
void pip_compose(int x, int y, int rw, int rh, int win_w, int win_h);
void rc_free(mpv_render_context *rc);
void load_fd(mpv_handle *m, const char *url);
void set_property(mpv_handle *m, const char *name, const char *value);
const char *gl_str(GLenum name);
int gl_max_tex(void);
int gl_error(void);
GLuint make_tex(void);
void free_tex(GLuint t);
void tex_alloc(GLuint t, int w, int h, int bgra);
void tex_rect(GLuint t, int x, int y, int w, int h, int stride_px, const void *p, int bgra);
void clear_to(int W, int H, float r, float g, float b);
void panel_begin(int W, int H);
void panel_tile(GLuint t, int x, int y, int w, int h, float u, float v);
void panel_end(void);
void fb_pixel(int x, int y, unsigned char *rgba);

bool e_repeat(SDL_Event *e);
void inject_repeat_burst(SDL_Keycode k, int count);

#endif
